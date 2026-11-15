package renovate

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// scrubLimits bound the walk over a working copy's .git. A copy that exceeds one is removed:
// a scrub that cannot finish never hands the copy on as clean.
type scrubLimits struct {
	// configBytes caps a git config file, which is read whole and rewritten.
	configBytes int64
	// fileBytes caps any other file searched for the token.
	fileBytes int64
	// entries caps the files and directories visited in one .git.
	entries int
	// depth caps how deep the walk goes below .git; submodules nest as modules/<name>/modules/<name>.
	depth int
}

var gitLimits = scrubLimits{configBytes: 1 << 20, fileBytes: 64 << 20, entries: 200_000, depth: 24}

// workingCopyDepth is how deep a working copy sits below baseDir/repos: platform/owner/repo.
const workingCopyDepth = 3

var errUnusableCopy = errors.New("working copy is not a plain git checkout")

// remoteUserInfo matches the credentials of a URL: scheme://user:password@.
var remoteUserInfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s"]*@`)

// scrubBaseDir removes installation credentials from every working copy Renovate keeps under
// baseDir (persistRepoData): the user-info of each URL and any extraheader in every git config
// under the copy's .git, which is where Renovate's clone and fetch URL carries the token. That
// is .git/config and config.worktree, and the same files of each submodule (.git/modules, at any
// depth up to a bound) and linked worktree (.git/worktrees): git resolves a relative submodule
// URL against the remote URL, token included, and writes the result into the submodule's own
// config. Renovate puts the next job's token back itself. Every job runs as the same user, so a
// token left here would be readable by the code of whatever repository is processed next in the
// slot.
//
// Afterwards the whole .git is searched for the token, wherever it is, and a copy that holds it
// is removed. Nothing is followed: an entry that is not a plain directory, a copy whose .git is
// not a plain directory, a config that is not a plain file, a copy that exceeds the walk's
// bounds, and a copy the scrub cannot rewrite, is removed instead. The returned error names the
// copies that could be neither scrubbed nor removed; it never holds a credential.
func scrubBaseDir(baseDir, token string) error {
	repos := filepath.Join(baseDir, "repos")
	info, err := os.Lstat(repos)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.Remove(repos)
	}
	return scrubTree(repos, workingCopyDepth, token)
}

func scrubTree(dir string, depth int, token string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case !entry.IsDir():
			// A symlink or file: remove the entry itself, never what a link points to.
			errs = append(errs, os.Remove(path))
		case depth > 1:
			errs = append(errs, scrubTree(path, depth-1, token))
		default:
			errs = append(errs, scrubCopy(path, token))
		}
	}
	return errors.Join(errs...)
}

func scrubCopy(dir, token string) error {
	if err := scrubGitDir(dir, token); err == nil {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("working copy %s could be neither scrubbed nor removed: %w", dir, err)
	}
	return nil
}

// gitWalk is one bounded, no-follow pass over a .git directory.
type gitWalk struct {
	needles [][]byte
	entries int
}

func scrubGitDir(dir, token string) error {
	gitDir := filepath.Join(dir, ".git")
	if info, err := os.Lstat(gitDir); err != nil || !info.IsDir() {
		return errUnusableCopy
	}
	if info, err := os.Lstat(filepath.Join(gitDir, "config")); err != nil || !info.Mode().IsRegular() {
		return errUnusableCopy
	}
	w := &gitWalk{}
	if token != "" {
		w.needles = [][]byte{
			[]byte(token),
			[]byte(base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))),
		}
	}
	return w.dir(gitDir, 0, false)
}

// dir strips every git config in path and searches every other file for the token. Every
// directory is visited and every config found is stripped, wherever it is. Only what sits below a
// git dir's own object store (inObjects) is not read: git stores that content deflated, so a
// token cannot be found in it. A directory named objects is not that store unless its parent is
// a git dir, because a submodule's git dir is .git/modules/<its path> and its path may end in
// objects; the walk never trusts a name to skip a config.
func (w *gitWalk) dir(path string, depth int, inObjects bool) error {
	if depth > gitLimits.depth {
		return errUnusableCopy
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if w.entries++; w.entries > gitLimits.entries {
			return errUnusableCopy
		}
		child := filepath.Join(path, entry.Name())
		isConfig := entry.Name() == "config" || entry.Name() == "config.worktree"
		var err error
		switch mode := entry.Type(); {
		case mode.IsDir():
			if isConfig {
				err = errUnusableCopy
			} else {
				err = w.dir(child, depth+1, inObjects || (entry.Name() == "objects" && isGitDir(path)))
			}
		case mode.IsRegular() && isConfig:
			err = w.config(path, child)
		case mode.IsRegular():
			if !inObjects {
				err = w.file(child)
			}
		case mode&os.ModeSymlink != 0 && !isConfig:
			// Never followed; what it points at is not searched, but its own text is.
			target, lerr := os.Readlink(child)
			if lerr != nil || holdsToken([]byte(target), w.needles) {
				err = errUnusableCopy
			}
		default:
			err = errUnusableCopy
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// isGitDir reports whether dir is a git dir, which has a HEAD file of its own. The files of an
// object store are not read on the strength of this, so a directory made to look like one hides
// no config: configs are stripped wherever they are.
func isGitDir(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, "HEAD"))
	return err == nil && info.Mode().IsRegular()
}

func (w *gitWalk) config(dir, path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(f, gitLimits.configBytes+1))
	_ = f.Close()
	if err != nil || int64(len(raw)) > gitLimits.configBytes {
		return errUnusableCopy
	}
	cleaned := stripCredentials(string(raw))
	if holdsToken([]byte(cleaned), w.needles) {
		return errUnusableCopy
	}
	if cleaned == string(raw) {
		return nil
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".scrub-")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(cleaned)
	cerr := tmp.Close()
	// The Renovate user shares the root group: it must be able to write the file back.
	merr := os.Chmod(tmp.Name(), 0o664)
	if err := errors.Join(werr, cerr, merr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// file searches one regular file for the token, in chunks that overlap by one needle's length.
func (w *gitWalk) file(path string) error {
	if len(w.needles) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > gitLimits.fileBytes {
		return errUnusableCopy
	}
	keep := 0
	for _, n := range w.needles {
		keep = max(keep, len(n)-1)
	}
	buf := make([]byte, keep+searchChunk)
	var read int64
	have := 0
	for {
		n, err := f.Read(buf[have:])
		have += n
		if read += int64(n); read > gitLimits.fileBytes {
			return errUnusableCopy
		}
		if holdsToken(buf[:have], w.needles) {
			return errUnusableCopy
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if have > keep {
			copy(buf, buf[have-keep:have])
			have = keep
		}
	}
}

// searchChunk is how much of a file is read at once.
const searchChunk = 1 << 20

// stripCredentials drops the user-info of every URL in a git config and every extraheader.
func stripCredentials(cfg string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(cfg, "\n") {
		key, _, _ := strings.Cut(line, "=")
		if strings.EqualFold(strings.TrimSpace(key), "extraheader") {
			continue
		}
		b.WriteString(remoteUserInfo.ReplaceAllString(line, "$1"))
	}
	return b.String()
}

// holdsToken reports whether the job's own token is in data, plain or as the basic-auth value
// of an Authorization header; needles are those two forms, and none means there is no token.
func holdsToken(data []byte, needles [][]byte) bool {
	for _, n := range needles {
		if bytes.Contains(data, n) {
			return true
		}
	}
	return false
}
