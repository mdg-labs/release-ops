package renovate_test

import (
	"context"
	"encoding/base64"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/renovate"
)

const crashedToken = "ghs_TokenLeftByAJobThatDiedBeforeItsScrub0123456"

func remoteConfig(token string) string {
	return "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = https://x-access-token:" + token +
		"@github.com/acme/repo-1.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n[branch \"main\"]\n\tremote = origin\n"
}

func slotBase(e *env, slot string) string {
	return filepath.Join(e.cacheRoot, "slot-"+slot, "base")
}

func plantCopy(t *testing.T, base, repo, config string) string {
	t.Helper()
	gitDir := filepath.Join(base, "repos", "github", filepath.FromSlash(repo), ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "..", "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(gitDir)
}

// filesHolding lists the files under dir, symlinks not followed, whose content holds any of needles.
func filesHolding(t *testing.T, dir string, needles ...string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, n := range needles {
			if strings.Contains(string(raw), n) {
				hits = append(hits, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func unlockTree(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
	})
}

// Renovate's GitHub platform clones with the installation token in the remote URL, and
// persistRepoData keeps that working copy on the slot's cache volume. The next job in the
// slot, for another repository, runs that repository's code as the same user: it must not be
// able to read the earlier job's token.
func TestAJobsTokenIsNotLeftWhereTheNextJobOfAnotherRepositoryCanReadIt(t *testing.T) {
	e := newEnv(t, `
case "$RENOVATE_REPOSITORIES" in
acme/repo-1)
	mkdir -p "$RENOVATE_BASE_DIR/repos/github/acme/repo-1/.git"
	printf '[remote "origin"]\n\turl = https://x-access-token:%s@github.com/acme/repo-1.git\n' "$RENOVATE_TOKEN" > "$RENOVATE_BASE_DIR/repos/github/acme/repo-1/.git/config"
	touch "$RENOVATE_BASE_DIR/repos/github/acme/repo-1/kept"
	;;
*)
	grep -rl "`+jobToken+`" "$RENOVATE_BASE_DIR" > "$OUT/found" 2>/dev/null
	touch "$OUT/second-ran"
	;;
esac`)
	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	other := job(0)
	other.RepositoryFullName = "other/repo-2"
	if res := e.h.Run(context.Background(), other); res.Err != nil {
		t.Fatal(res.Err)
	}
	if !e.exists("second-ran") {
		t.Fatal("the second job did not run")
	}
	if found := strings.TrimSpace(e.read("found")); found != "" {
		t.Errorf("the second job found the first job's token in %s", found)
	}
	if hits := filesHolding(t, slotBase(e, "0"), jobToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
}

// The working copy stays usable for the next fetch: it is kept, with its remote, minus the
// credentials.
func TestScrubKeepsTheWorkingCopyAndEverythingButTheCredentials(t *testing.T) {
	for name, tc := range map[string]struct{ written, kept, gone string }{
		"user-info in the remote": {
			written: remoteConfig(jobToken),
			kept:    "url = https://github.com/acme/repo-1.git",
		},
		"user-info in an insteadOf section": {
			written: "[url \"https://x-access-token:" + jobToken + "@github.com/\"]\n\tinsteadOf = https://github.com/\n[core]\n\tbare = false\n",
			kept:    "bare = false",
		},
		"an extra header": {
			written: "[http \"https://github.com/\"]\n\textraheader = AUTHORIZATION: basic " +
				base64.StdEncoding.EncodeToString([]byte("x-access-token:"+jobToken)) + "\n[core]\n\tbare = false\n",
			kept: "bare = false",
		},
		"a plain-http remote with a password": {
			written: "[remote \"origin\"]\n\turl = http://user:" + jobToken + "@ghe.example/acme/repo-1.git\n",
			kept:    "url = http://ghe.example/acme/repo-1.git",
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, `
d="$RENOVATE_BASE_DIR/repos/github/acme/repo-1"
mkdir -p "$d/.git"
cat > "$d/.git/config" <<'CFG'
`+tc.written+`CFG
touch "$d/file"`)
			if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
				t.Fatal(res.Err)
			}
			base := slotBase(e, "0")
			if hits := filesHolding(t, base, jobToken, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+jobToken))); len(hits) != 0 {
				t.Errorf("credentials are left in %v", hits)
			}
			raw, err := os.ReadFile(filepath.Join(base, "repos", "github", "acme", "repo-1", ".git", "config"))
			if err != nil {
				t.Fatalf("the working copy's config is gone: %v", err)
			}
			if !strings.Contains(string(raw), tc.kept) {
				t.Errorf("config = %q, want it to keep %q", raw, tc.kept)
			}
			if _, err := os.Stat(filepath.Join(base, "repos", "github", "acme", "repo-1", "file")); err != nil {
				t.Errorf("the working copy was not kept: %v", err)
			}
			if info, err := os.Stat(filepath.Join(base, "repos", "github", "acme", "repo-1", ".git", "config")); err == nil && info.Mode().Perm()&0o060 != 0o060 {
				t.Errorf("config mode = %v: the Renovate user's group cannot write it", info.Mode().Perm())
			}
		})
	}
}

// A worker that died mid-job never ran its scrub; the next job's scrub runs before Renovate.
func TestAnEarlierJobsTokenIsScrubbedBeforeRenovateStarts(t *testing.T) {
	e := newEnv(t, `
grep -rl "`+crashedToken+`" "$RENOVATE_BASE_DIR" > "$OUT/found" 2>/dev/null
touch "$OUT/started"`)
	plantCopy(t, slotBase(e, "0"), "acme/repo-1", remoteConfig(crashedToken))
	plantCopy(t, slotBase(e, "0"), "other/repo-2", remoteConfig(crashedToken))
	plantCopy(t, slotBase(e, "1"), "acme/repo-1", remoteConfig(crashedToken))

	res := e.h.Run(context.Background(), job(0))
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !e.exists("started") {
		t.Fatal("Renovate did not run")
	}
	if found := strings.TrimSpace(e.read("found")); found != "" {
		t.Errorf("Renovate started with the earlier job's token in %s", found)
	}
	if _, err := os.Stat(filepath.Join(slotBase(e, "0"), "repos", "github", "other", "repo-2", "package.json")); err != nil {
		t.Errorf("the other working copy was not kept: %v", err)
	}
	// Another slot's directory is that slot's own to scrub.
	if hits := filesHolding(t, slotBase(e, "1"), crashedToken); len(hits) != 1 {
		t.Errorf("slot 1's copy was touched by a job in slot 0: %v", hits)
	}
}

// Whatever ends the run, the token the run wrote is gone afterwards.
func TestTheScrubRunsAfterEveryKindOfRunEnd(t *testing.T) {
	write := `
d="$RENOVATE_BASE_DIR/repos/github/acme/repo-1"
mkdir -p "$d/.git"
printf '[remote "origin"]\n\turl = https://x-access-token:%s@github.com/acme/repo-1.git\n' "$RENOVATE_TOKEN" > "$d/.git/config"
`
	for name, tc := range map[string]struct {
		script string
		opts   envOption
	}{
		"failure": {script: write + "exit 3"},
		"timeout": {script: write + "exec sleep 60", opts: func(o *renovate.Options) {
			o.Deadline = 400 * time.Millisecond
			o.MinRun = 50 * time.Millisecond
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var opts []envOption
			if tc.opts != nil {
				opts = append(opts, tc.opts)
			}
			e := newEnv(t, tc.script, opts...)
			res := e.h.Run(context.Background(), job(0))
			if res.Err == nil {
				t.Fatal("the run was expected to fail")
			}
			if hits := filesHolding(t, slotBase(e, "0"), jobToken); len(hits) != 0 {
				t.Errorf("the token is left in %v", hits)
			}
			secretFree(t, e, res)
		})
	}

	t.Run("abort", func(t *testing.T) {
		e := newEnv(t, write+`touch "$OUT/written"; exec sleep 60`)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); e.h.Run(ctx, job(0)) }()
		deadline := time.Now().Add(3 * time.Second)
		for !e.exists("written") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the run did not stop on abort")
		}
		if hits := filesHolding(t, slotBase(e, "0"), jobToken); len(hits) != 0 {
			t.Errorf("the token is left in %v", hits)
		}
	})
}

// A repository's code can plant symlinks in a working copy. The scrub never follows one and
// writes nothing outside the base directory: it removes what it cannot treat as a plain copy.
func TestTheScrubDoesNotFollowSymlinks(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	outside := t.TempDir()
	outsideConfig := filepath.Join(outside, "config")
	original := remoteConfig(crashedToken)
	if err := os.WriteFile(outsideConfig, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideCopy := filepath.Join(outside, "copy")
	if err := os.MkdirAll(filepath.Join(outsideCopy, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideCopy, ".git", "config"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	base := slotBase(e, "0")
	// A copy whose .git/config is a symlink, one whose .git is, one that is itself a symlink, and
	// an owner directory that is.
	cfgLink := plantCopy(t, base, "acme/config-link", "")
	mustSymlink(t, outsideConfig, filepath.Join(cfgLink, ".git", "config"), true)
	gitLink := filepath.Join(base, "repos", "github", "acme", "git-link")
	if err := os.MkdirAll(gitLink, 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, filepath.Join(outsideCopy, ".git"), filepath.Join(gitLink, ".git"), false)
	mustSymlink(t, outsideCopy, filepath.Join(base, "repos", "github", "acme", "copy-link"), false)
	mustSymlink(t, outside, filepath.Join(base, "repos", "github", "linked-owner"), false)
	ok := plantCopy(t, base, "acme/ok", remoteConfig(crashedToken))

	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if !e.exists("started") {
		t.Fatal("Renovate did not run")
	}
	for _, p := range []string{outsideConfig, filepath.Join(outsideCopy, ".git", "config")} {
		raw, err := os.ReadFile(p)
		if err != nil || string(raw) != original {
			t.Errorf("%s was changed through a symlink: %q, %v", p, raw, err)
		}
	}
	for _, p := range []string{cfgLink, gitLink, filepath.Join(base, "repos", "github", "acme", "copy-link"), filepath.Join(base, "repos", "github", "linked-owner")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s was not removed", p)
		}
	}
	if hits := filesHolding(t, base, crashedToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
	if _, err := os.Stat(filepath.Join(ok, "package.json")); err != nil {
		t.Errorf("the plain working copy was not kept: %v", err)
	}
}

func mustSymlink(t *testing.T, target, link string, replace bool) {
	t.Helper()
	if replace {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// Fail closed: a working copy whose credentials can be neither stripped nor removed stops the
// job before Renovate runs.
func TestAScrubThatCannotCompleteStopsTheJobBeforeRenovateStarts(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	e := newEnv(t, `touch "$OUT/started"`)
	base := slotBase(e, "0")
	copyDir := plantCopy(t, base, "acme/repo-1", remoteConfig(crashedToken))
	unlockTree(t, base)
	for _, d := range []string{filepath.Join(copyDir, ".git"), filepath.Dir(copyDir)} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}

	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeCacheScrub)
	if e.exists("started") {
		t.Error("Renovate ran with a working copy that could not be scrubbed")
	}
	if strings.Contains(e.logs.String(), crashedToken) || strings.Contains(res.Err.Error(), crashedToken) {
		t.Error("the earlier token reached the logs or the reason")
	}
}

// After the run the same applies: a token that cannot be cleared fails the job, and the next
// job's scrub, which retries, does not let Renovate start while it is there.
func TestAScrubThatCannotCompleteAfterTheRunFailsTheJobAndTheNextOne(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	e := newEnv(t, `
if [ -e "$OUT/first-done" ]; then touch "$OUT/second-started"; exit 0; fi
d="$RENOVATE_BASE_DIR/repos/github/acme/repo-1"
mkdir -p "$d/.git"
printf '[remote "origin"]\n\turl = https://x-access-token:%s@github.com/acme/repo-1.git\n' "$RENOVATE_TOKEN" > "$d/.git/config"
chmod 555 "$d/.git" "$d/.." 
touch "$OUT/first-done"`)
	unlockTree(t, slotBase(e, "0"))

	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeCacheScrub)
	secretFree(t, e, res)
	res = e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeCacheScrub)
	if e.exists("second-started") {
		t.Error("Renovate started over a working copy that still held a token")
	}
	secretFree(t, e, res)
}

// moduleConfig is a submodule's own config as git writes it: the remote URL it resolved against
// the superproject's token-bearing remote.
func moduleConfig(token, name string) string {
	return "[core]\n\tbare = false\n\tworktree = ../../../" + name + "\n[remote \"origin\"]\n\turl = https://x-access-token:" +
		token + "@github.com/acme/" + name + ".git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every git config under a copy's .git is scrubbed, not only .git/config: a submodule's own
// config, at any nesting, a worktree's, and config.worktree.
func TestEveryGitConfigUnderACopyIsScrubbed(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	base := slotBase(e, "0")
	copyDir := plantCopy(t, base, "acme/repo-1", remoteConfig(crashedToken))
	gitDir := filepath.Join(copyDir, ".git")
	paths := []string{
		filepath.Join(gitDir, "modules", "shared", "config"),
		filepath.Join(gitDir, "modules", "shared", "config.worktree"),
		filepath.Join(gitDir, "modules", "shared", "modules", "nested", "config"),
		filepath.Join(gitDir, "modules", "shared", "modules", "nested", "modules", "deep", "config"),
		filepath.Join(gitDir, "worktrees", "wt", "config.worktree"),
		filepath.Join(gitDir, "config.worktree"),
	}
	for _, p := range paths {
		writeFile(t, p, moduleConfig(crashedToken, "shared"))
	}
	writeFile(t, filepath.Join(gitDir, "modules", "shared", "objects", "pack", "pack-1.pack"), "PACK")

	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if hits := filesHolding(t, base, crashedToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if !strings.Contains(string(raw), "url = https://github.com/acme/shared.git") || !strings.Contains(string(raw), "bare = false") {
			t.Errorf("%s = %q, want the config kept without its credentials", p, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(copyDir, "package.json")); err != nil {
		t.Errorf("the working copy was not kept: %v", err)
	}
}

// The scrub does not trust that it found every place a token went: after stripping, a copy in
// whose .git the token is still found, in any file, is removed.
func TestACopyThatStillHoldsTheTokenAfterTheScrubIsRemoved(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + jobToken))
	for name, plant := range map[string]func(gitDir string){
		"a plain file":          func(g string) { writeFile(t, filepath.Join(g, "some-file"), "x "+jobToken+" y") },
		"the basic-auth form":   func(g string) { writeFile(t, filepath.Join(g, "logs", "note"), "Authorization: basic "+b64) },
		"a file in a submodule": func(g string) { writeFile(t, filepath.Join(g, "modules", "shared", "FETCH_HEAD"), jobToken) },
		"a config value that is no URL": func(g string) {
			writeFile(t, filepath.Join(g, "modules", "shared", "config"), "[x]\n\tkey = "+jobToken+"\n")
		},
		"a symlink's text": func(g string) {
			if err := os.Symlink(jobToken, filepath.Join(g, "link")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, `touch "$OUT/started"`)
			base := slotBase(e, "0")
			bad := plantCopy(t, base, "acme/repo-1", remoteConfig(jobToken))
			plant(filepath.Join(bad, ".git"))
			good := plantCopy(t, base, "acme/repo-2", remoteConfig(jobToken))

			if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
				t.Fatal(res.Err)
			}
			if _, err := os.Lstat(bad); err == nil {
				t.Error("the copy that still holds the token was kept")
			}
			if _, err := os.Stat(filepath.Join(good, "package.json")); err != nil {
				t.Errorf("the clean copy was not kept: %v", err)
			}
			if hits := filesHolding(t, base, jobToken, b64); len(hits) != 0 {
				t.Errorf("the token is left in %v", hits)
			}
		})
	}
}

// git, run against local bare repositories only, is what really writes a submodule's config.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "protocol.file.allow=always", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// remotesWithSubmodule builds bare repositories acme/repo-1, which has the submodule ../shared.git
// at the path subPath (git also names the submodule that under .git/modules), which has the
// submodule ../nested.git, and returns the directory holding them.
func remotesWithSubmodule(t *testing.T, subPath string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "bare")
	work := filepath.Join(root, "work")
	for _, d := range []string{filepath.Join(bare, "acme"), work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []string{"nested", "shared", "repo-1"} {
		gitIn(t, bare, "init", "-q", "--bare", filepath.Join("acme", r+".git"))
	}
	gitIn(t, work, "clone", "-q", filepath.Join(bare, "acme", "nested.git"), "nested")
	writeFile(t, filepath.Join(work, "nested", "f"), "n")
	gitIn(t, filepath.Join(work, "nested"), "add", "f")
	gitIn(t, filepath.Join(work, "nested"), "commit", "-qm", "n")
	gitIn(t, filepath.Join(work, "nested"), "push", "-q", "origin", "HEAD:main")
	gitIn(t, work, "clone", "-q", filepath.Join(bare, "acme", "shared.git"), "shared")
	gitIn(t, filepath.Join(work, "shared"), "submodule", "add", "-q", "../nested.git", "nested")
	gitIn(t, filepath.Join(work, "shared"), "commit", "-qm", "s")
	gitIn(t, filepath.Join(work, "shared"), "push", "-q", "origin", "HEAD:main")
	gitIn(t, work, "clone", "-q", filepath.Join(bare, "acme", "repo-1.git"), "repo-1")
	gitIn(t, filepath.Join(work, "repo-1"), "submodule", "add", "-q", "../shared.git", subPath)
	gitIn(t, filepath.Join(work, "repo-1"), "commit", "-qm", "r")
	gitIn(t, filepath.Join(work, "repo-1"), "push", "-q", "origin", "HEAD:main")
	return bare
}

// Renovate's cloneSubmodules runs `git submodule update --init --recursive`. Git resolves a
// relative submodule URL against the superproject's remote, token included, and writes it into
// the submodule's own config under .git/modules, nested ones too. The next job of another
// repository in the slot must not find it.
func TestASubmodulesTokenIsNotLeftWhereTheNextJobCanReadIt(t *testing.T) {
	assertASubmodulesTokenIsScrubbed(t, "shared")
}

// A submodule's git dir is .git/modules/<its path>, so a submodule whose path ends in "objects"
// keeps its config in a directory that is named like an object store but is not one.
func TestASubmoduleNamedObjectsHasItsTokenScrubbed(t *testing.T) {
	for _, subPath := range []string{"objects", "vendor/objects", "assets/objects", "a/objects/b"} {
		t.Run(subPath, func(t *testing.T) { assertASubmodulesTokenIsScrubbed(t, subPath) })
	}
}

func assertASubmodulesTokenIsScrubbed(t *testing.T, subPath string) {
	t.Helper()
	requireGit(t)
	bare := remotesWithSubmodule(t, subPath)
	e := newEnv(t, `
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_TERMINAL_PROMPT=0
export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0="url.file://`+bare+`/.insteadOf" GIT_CONFIG_VALUE_0="https://x-access-token:$RENOVATE_TOKEN@github.com/"
export GIT_CONFIG_KEY_1=protocol.file.allow GIT_CONFIG_VALUE_1=always
case "$RENOVATE_REPOSITORIES" in
acme/repo-1)
	d="$RENOVATE_BASE_DIR/repos/github/acme/repo-1"
	git clone -q "https://x-access-token:$RENOVATE_TOKEN@github.com/acme/repo-1.git" "$d" || exit 10
	(cd "$d" && git submodule update --init --recursive -q) || exit 11
	grep -rl "$RENOVATE_TOKEN" "$d/.git/modules" > "$OUT/written-by-git" 2>/dev/null
	;;
*)
	grep -rl "`+jobToken+`" "$RENOVATE_BASE_DIR" > "$OUT/found" 2>/dev/null
	touch "$OUT/second-ran"
	;;
esac`)
	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatalf("%v\n%s", res.Err, e.logs.String())
	}
	if n := strings.Count(e.read("written-by-git"), "\n"); n < 2 {
		t.Fatalf("git put the token in %d submodule configs, want the nested one too; the test does not reproduce the leak:\n%s", n, e.read("written-by-git"))
	}
	if hits := filesHolding(t, slotBase(e, "0"), jobToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}

	other := job(0)
	other.RepositoryFullName = "other/repo-2"
	if res := e.h.Run(context.Background(), other); res.Err != nil {
		t.Fatal(res.Err)
	}
	if !e.exists("second-ran") {
		t.Fatal("the second job did not run")
	}
	if found := strings.TrimSpace(e.read("found")); found != "" {
		t.Errorf("the second job found the first job's token in %s", found)
	}
	if hits := filesHolding(t, slotBase(e, "0"), jobToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
	// The copy and its submodules stay usable for the next fetch.
	if _, err := os.Stat(filepath.Join(slotBase(e, "0"), "repos", "github", "acme", "repo-1", ".git", "modules", filepath.FromSlash(subPath), "modules", "nested", "config")); err != nil {
		t.Errorf("the working copy was not kept: %v", err)
	}
}

// A copy deeper than the bound is removed rather than trusted.
func TestACopyNestedBeyondTheWalkBoundIsRemoved(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	base := slotBase(e, "0")
	deep := plantCopy(t, base, "acme/deep", remoteConfig(crashedToken))
	p := filepath.Join(deep, ".git")
	for i := 0; i < 40; i++ {
		p = filepath.Join(p, "modules", "m")
	}
	writeFile(t, filepath.Join(p, "config"), moduleConfig(crashedToken, "m"))
	ok := plantCopy(t, base, "acme/ok", remoteConfig(crashedToken))

	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := os.Lstat(deep); err == nil {
		t.Error("the copy nested beyond the bound was kept")
	}
	if _, err := os.Stat(filepath.Join(ok, "package.json")); err != nil {
		t.Errorf("the ordinary copy was not kept: %v", err)
	}
	if hits := filesHolding(t, base, crashedToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
}

// A config is a config wherever git or a submodule's name puts it, including in a directory named
// objects: below .git, below .git/modules, and inside a submodule's own object store.
func TestAConfigInADirectoryNamedObjectsIsScrubbed(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	base := slotBase(e, "0")
	copyDir := plantCopy(t, base, "acme/repo-1", remoteConfig(crashedToken))
	gitDir := filepath.Join(copyDir, ".git")
	for _, f := range []string{"HEAD"} {
		writeFile(t, filepath.Join(gitDir, f), "ref: refs/heads/main\n")
		writeFile(t, filepath.Join(gitDir, "modules", "real", f), "ref: refs/heads/main\n")
	}
	paths := []string{
		filepath.Join(gitDir, "modules", "objects", "config"),
		filepath.Join(gitDir, "modules", "a", "modules", "objects", "config"),
		filepath.Join(gitDir, "modules", "vendor", "objects", "config"),
		filepath.Join(gitDir, "modules", "vendor", "objects", "config.worktree"),
		filepath.Join(gitDir, "modules", "objects", "modules", "nested", "config"),
		// The object store of a git dir holds no config, but one planted there is stripped too.
		filepath.Join(gitDir, "objects", "config"),
		filepath.Join(gitDir, "modules", "real", "objects", "config"),
		filepath.Join(gitDir, "modules", "real", "objects", "pack", "config"),
	}
	for _, p := range paths {
		writeFile(t, p, moduleConfig(crashedToken, "shared"))
	}
	// What a real object store holds is not searched, so it stays as it is.
	writeFile(t, filepath.Join(gitDir, "objects", "pack", "pack-1.pack"), "PACK")
	writeFile(t, filepath.Join(gitDir, "objects", "ab", "cdef"), "LOOSE")

	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if hits := filesHolding(t, base, crashedToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if !strings.Contains(string(raw), "url = https://github.com/acme/shared.git") {
			t.Errorf("%s = %q, want the config kept without its credentials", p, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(gitDir, "objects", "ab", "cdef")); err != nil {
		t.Errorf("the object store was not kept: %v", err)
	}
}

// The search for a token that is still left covers a directory named objects that is not an
// object store, and a config under it that cannot be scrubbed removes the copy.
func TestATokenInASubmoduleNamedObjectsRemovesTheCopy(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	base := slotBase(e, "0")
	copyDir := plantCopy(t, base, "acme/repo-1", remoteConfig(jobToken))
	writeFile(t, filepath.Join(copyDir, ".git", "modules", "objects", "FETCH_HEAD"), jobToken)
	writeFile(t, filepath.Join(copyDir, ".git", "modules", "objects", "config"), moduleConfig(jobToken, "x"))
	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := os.Lstat(copyDir); err == nil {
		t.Error("the copy that still held the token was kept")
	}
	if hits := filesHolding(t, base, jobToken); len(hits) != 0 {
		t.Errorf("the token is left in %v", hits)
	}
}
