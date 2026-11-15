// Package globalconfig finds Renovate's global (self-hosted) configuration and its tools
// directory: either mounted into the worker or fetched from a repository with a job's
// installation token (specs.html § 2.4, § 2.5). Release Ops never evaluates the file, which
// may be JavaScript, and never interprets its allowedCommands: this package only moves bytes
// and bounds them. It depends on nothing else of Release Ops, so the worker's handler and the
// app's raw config display share it.
package globalconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// Dir is the directory of the config repository that holds the global config and the tools.
	Dir = "renovate-global"
	// ToolsDirName is the tools directory inside Dir.
	ToolsDirName = "tools"
	// MountConfigDir and MountToolsDir are where the mounted variant looks.
	MountConfigDir = "/config"
	MountToolsDir  = "/opt/renovate-tools"

	maxConfigBytes   = 1 << 20
	maxToolFileBytes = 1 << 20
	maxToolsBytes    = 8 << 20
	maxToolFiles     = 100
	maxToolsDepth    = 4
	maxListingBytes  = 1 << 20
	maxFileDocBytes  = maxToolFileBytes*4/3 + 64<<10
)

// ConfigNames are the file names the global config is looked up under, in order.
var ConfigNames = []string{"config.js", "config.json"}

// OwnedKeys are the settings Release Ops sets itself per job: a global config that holds one
// has it ignored (specs.html § 2.4).
var OwnedKeys = []string{"platform", "token", "repositories", "allowScripts"}

// Source says where a Config came from.
type Source string

const (
	SourceRepository Source = "repository"
	SourceMounted    Source = "mounted"
)

// Errors a lookup can end with. None carries a token.
var (
	// ErrNotMounted is returned by Mounted when the mount holds no config file.
	ErrNotMounted = errors.New("no global config is mounted")
	// ErrNoConfig is returned by Fetch when the repository has no config file in Dir.
	ErrNoConfig = errors.New("the config repository has no global config file")
	// ErrTooLarge is returned when a file or the tools directory exceeds its cap.
	ErrTooLarge = errors.New("the global config or its tools exceed the size limit")
)

// Config is a global config found at a path the worker can hand to Renovate.
type Config struct {
	Source Source
	// Path is the absolute path of the config file.
	Path string
	// Raw is the file's text as found.
	Raw string
	// ToolsDir is the tools directory, empty when there is none.
	ToolsDir string
}

// Mounted reads the config the host mounted: the first of ConfigNames in configDir, and
// toolsDir when it exists. ErrNotMounted means there is no config file there.
func Mounted(configDir, toolsDir string) (*Config, error) {
	for _, name := range ConfigNames {
		p := filepath.Join(configDir, name)
		info, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("global config mount: %w", err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("global config mount: %s is not a regular file", name)
		}
		if info.Size() > maxConfigBytes {
			return nil, ErrTooLarge
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("global config mount: %w", err)
		}
		cfg := &Config{Source: SourceMounted, Path: p, Raw: string(raw)}
		if info, err := os.Stat(toolsDir); err == nil && info.IsDir() {
			cfg.ToolsDir = toolsDir
		}
		return cfg, nil
	}
	return nil, ErrNotMounted
}

// FetchOptions say what to fetch and where to put it.
type FetchOptions struct {
	// HTTP is the client; its redirects are not followed, so the token goes to APIBase only.
	HTTP *http.Client
	// APIBase is the GitHub REST API base URL, without a trailing slash.
	APIBase string
	// Token is the job's installation token; it authorizes the reads and is never stored.
	Token string
	// Repo is owner/repo of the config repository and Ref the branch, tag or commit to read.
	Repo, Ref string
	// Dest is an existing directory the config and tools are written under.
	Dest string
}

// Fetch downloads the global config and the tools directory from the repository's Dir at Ref
// into opts.Dest. The files are size-capped and written as plain files; nothing is executed.
func Fetch(ctx context.Context, opts FetchOptions) (*Config, error) {
	f, err := newFetcher(opts)
	if err != nil {
		return nil, err
	}
	entries, err := f.list(ctx, Dir)
	if err != nil {
		return nil, err
	}
	var cfgEntry *entry
	for _, name := range ConfigNames {
		if i := slices.IndexFunc(entries, func(e entry) bool { return e.Type == "file" && e.Name == name }); i >= 0 {
			cfgEntry = &entries[i]
			break
		}
	}
	if cfgEntry == nil {
		return nil, ErrNoConfig
	}
	raw, err := f.file(ctx, cfgEntry.Path, maxConfigBytes)
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(opts.Dest, cfgEntry.Name)
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		return nil, fmt.Errorf("write global config: %w", err)
	}
	cfg := &Config{Source: SourceRepository, Path: cfgPath, Raw: string(raw)}

	if i := slices.IndexFunc(entries, func(e entry) bool { return e.Type == "dir" && e.Name == ToolsDirName }); i >= 0 {
		toolsDir := filepath.Join(opts.Dest, ToolsDirName)
		if err := f.tools(ctx, entries[i].Path, toolsDir, 0); err != nil {
			return nil, err
		}
		cfg.ToolsDir = toolsDir
	}
	return cfg, nil
}

type fetcher struct {
	opts  FetchOptions
	base  string
	files int
	bytes int
}

type entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func newFetcher(opts FetchOptions) (*fetcher, error) {
	owner, name, ok := strings.Cut(opts.Repo, "/")
	if !ok || owner == "" || name == "" || strings.ContainsAny(opts.Repo, " \t\r\n?#\\") || strings.Count(opts.Repo, "/") != 1 {
		return nil, fmt.Errorf("global config repository %q is not owner/repo", opts.Repo)
	}
	if opts.Ref == "" || strings.ContainsAny(opts.Ref, "\r\n\x00") {
		return nil, errors.New("global config ref is empty or malformed")
	}
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{}
	}
	hc := *opts.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	opts.HTTP = &hc
	return &fetcher{
		opts: opts,
		base: strings.TrimRight(opts.APIBase, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/contents/",
	}, nil
}

func (f *fetcher) get(ctx context.Context, repoPath string, limit int64) ([]byte, error) {
	segments := strings.Split(repoPath, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	u := f.base + strings.Join(segments, "/") + "?ref=" + url.QueryEscape(f.opts.Ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+f.opts.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := f.opts.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("fetch %s: the GitHub API is unreachable", repoPath)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %d", repoPath, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", repoPath, err)
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	return body, nil
}

func (f *fetcher) list(ctx context.Context, dir string) ([]entry, error) {
	body, err := f.get(ctx, dir, maxListingBytes)
	if err != nil {
		return nil, err
	}
	var entries []entry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("fetch %s: not a directory listing", dir)
	}
	for _, e := range entries {
		if !safeName(e.Name) || path.Join(dir, e.Name) != e.Path {
			return nil, fmt.Errorf("fetch %s: unsafe entry name", dir)
		}
	}
	return entries, nil
}

func (f *fetcher) file(ctx context.Context, repoPath string, limit int) ([]byte, error) {
	body, err := f.get(ctx, repoPath, maxFileDocBytes)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		Size     int    `json:"size"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Type != "file" {
		return nil, fmt.Errorf("fetch %s: not a file", repoPath)
	}
	if doc.Size > limit {
		return nil, ErrTooLarge
	}
	if doc.Encoding != "base64" {
		return nil, fmt.Errorf("fetch %s: content is not inline", repoPath)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(doc.Content))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: content is not base64", repoPath)
	}
	if len(raw) > limit {
		return nil, ErrTooLarge
	}
	return raw, nil
}

// tools mirrors a directory of the repository under dest: directories 0755, files 0755 so a
// tool can run. Symlinks and submodules are skipped by the API as non-file entries and refused.
func (f *fetcher) tools(ctx context.Context, repoDir, dest string, depth int) error {
	if depth > maxToolsDepth {
		return ErrTooLarge
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("write tools: %w", err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return fmt.Errorf("write tools: %w", err)
	}
	entries, err := f.list(ctx, repoDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		switch e.Type {
		case "dir":
			if err := f.tools(ctx, e.Path, filepath.Join(dest, e.Name), depth+1); err != nil {
				return err
			}
		case "file":
			f.files++
			if f.files > maxToolFiles {
				return ErrTooLarge
			}
			raw, err := f.file(ctx, e.Path, maxToolFileBytes)
			if err != nil {
				return err
			}
			if f.bytes += len(raw); f.bytes > maxToolsBytes {
				return ErrTooLarge
			}
			p := filepath.Join(dest, e.Name)
			if err := os.WriteFile(p, raw, 0o755); err != nil {
				return fmt.Errorf("write tools: %w", err)
			}
		default:
			return fmt.Errorf("fetch %s: %s is a %s, not a file or directory", repoDir, e.Name, e.Type)
		}
	}
	return nil
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00")
}

// WithoutOwnedKeys returns the path of the config as Renovate should read it. A JSON config
// that sets a key of OwnedKeys, at the top level or inside its "force" object (which Renovate
// applies over the merged settings), is rewritten into dir without it; the removed key names
// are returned, those inside "force" as "force.<key>". A "force" that is not an object is left
// as it is. Any other config is returned as it is, which is also what happens to JSON the
// worker cannot parse and to a JavaScript config, whose file the worker cannot rewrite without
// evaluating it. The per-job environment sets the same four settings and wins over the file
// in Renovate, so a key left in a file the worker cannot rewrite is still overridden except
// under "force". Values are never read.
func (c *Config) WithoutOwnedKeys(dir string) (string, []string, error) {
	if filepath.Ext(c.Path) != ".json" {
		return c.Path, nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader([]byte(c.Raw))).Decode(&doc); err != nil {
		return c.Path, nil, nil
	}
	var removed []string
	for _, key := range OwnedKeys {
		if _, ok := doc[key]; ok {
			delete(doc, key)
			removed = append(removed, key)
		}
	}
	if rawForce, ok := doc["force"]; ok {
		var force map[string]json.RawMessage
		if err := json.Unmarshal(rawForce, &force); err == nil && force != nil {
			var forced []string
			for _, key := range OwnedKeys {
				if _, ok := force[key]; ok {
					delete(force, key)
					forced = append(forced, "force."+key)
				}
			}
			if len(forced) > 0 {
				raw, err := json.Marshal(force)
				if err != nil {
					return "", nil, err
				}
				doc["force"] = raw
				removed = append(removed, forced...)
			}
		}
	}
	if len(removed) == 0 {
		return c.Path, nil, nil
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		return "", nil, fmt.Errorf("write global config: %w", err)
	}
	return p, removed, nil
}
