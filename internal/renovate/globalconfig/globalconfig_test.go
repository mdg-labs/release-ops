package globalconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const testToken = "ghs_globalconfigtesttoken0123456789"

// fakeContents is a fake of GET /repos/{owner}/{repo}/contents/{path}?ref=: files maps a path
// in the repository to its content, and a directory is whatever is a prefix of a path.
type fakeContents struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string]string
	kinds map[string]string // path -> non-default entry type, e.g. symlink
	seen  []string
	auth  []string
	sizes map[string]int
}

func newFakeContents(t *testing.T, files map[string]string) *fakeContents {
	t.Helper()
	f := &fakeContents{files: files, kinds: map[string]string{}, sizes: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeContents) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.URL.Path+"?ref="+r.URL.Query().Get("ref"))
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()

	prefix := "/repos/acme/renovate/contents/"
	if r.Header.Get("Authorization") != "Bearer "+testToken || !strings.HasPrefix(r.URL.Path, prefix) || r.URL.Query().Get("ref") != "release/1" {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, prefix)
	if content, ok := f.files[p]; ok {
		size := len(content)
		if s, ok := f.sizes[p]; ok {
			size = s
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "file", "encoding": "base64", "size": size, "name": path.Base(p), "path": p,
			"content": base64.StdEncoding.EncodeToString([]byte(content)),
		})
		return
	}
	seen := map[string]bool{}
	var list []map[string]any
	for fp := range f.files {
		rest, ok := strings.CutPrefix(fp, p+"/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if seen[name] {
			continue
		}
		seen[name] = true
		typ := "file"
		if nested {
			typ = "dir"
		}
		if k, ok := f.kinds[p+"/"+name]; ok {
			typ = k
		}
		list = append(list, map[string]any{"name": name, "path": p + "/" + name, "type": typ})
	}
	if len(list) == 0 {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func fetchOpts(f *fakeContents, dest string) FetchOptions {
	return FetchOptions{APIBase: f.srv.URL, Token: testToken, Repo: "acme/renovate", Ref: "release/1", Dest: dest}
}

func TestFetchWritesTheConfigAndToolsFromTheRefWithTheToken(t *testing.T) {
	f := newFakeContents(t, map[string]string{
		"renovate-global/config.json":       `{"onboarding": false, "allowedCommands": ["^hello\\.sh$"]}`,
		"renovate-global/tools/hello.sh":    "#!/bin/sh\necho hi\n",
		"renovate-global/tools/lib/util.sh": "echo util\n",
		"renovate-global/README.md":         "not fetched",
		"other-dir/config.json":             "not fetched",
	})
	dest := t.TempDir()

	cfg, err := Fetch(context.Background(), fetchOpts(f, dest))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if cfg.Source != SourceRepository || cfg.Path != filepath.Join(dest, "config.json") {
		t.Errorf("cfg = %+v", cfg)
	}
	if !strings.Contains(cfg.Raw, `"allowedCommands"`) {
		t.Errorf("Raw = %q", cfg.Raw)
	}
	if cfg.ToolsDir != filepath.Join(dest, "tools") {
		t.Fatalf("ToolsDir = %q", cfg.ToolsDir)
	}
	for rel, wantMode := range map[string]os.FileMode{"tools/hello.sh": 0o755, "tools/lib/util.sh": 0o755, "config.json": 0o644} {
		info, err := os.Stat(filepath.Join(dest, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if info.Mode().Perm() != wantMode {
			t.Errorf("%s mode = %v, want %v", rel, info.Mode().Perm(), wantMode)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); !os.IsNotExist(err) {
		t.Errorf("a file outside the config and tools was written: %v", err)
	}
	for _, a := range f.auth {
		if a != "Bearer "+testToken {
			t.Errorf("a request carried Authorization %q", a)
		}
	}
	for _, s := range f.seen {
		if !strings.HasSuffix(s, "?ref=release/1") {
			t.Errorf("a request did not name the ref: %q", s)
		}
	}
}

func TestFetchPrefersConfigJsOverJson(t *testing.T) {
	f := newFakeContents(t, map[string]string{
		"renovate-global/config.js":   "module.exports = {}",
		"renovate-global/config.json": "{}",
	})
	cfg, err := Fetch(context.Background(), fetchOpts(f, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cfg.Path) != "config.js" || cfg.ToolsDir != "" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestFetchFailsWithoutLeakingTheToken(t *testing.T) {
	f := newFakeContents(t, map[string]string{"renovate-global/readme": "x"})
	dest := t.TempDir()

	for name, tc := range map[string]struct {
		mutate func(*FetchOptions)
		want   error
	}{
		"no config file":   {func(*FetchOptions) {}, ErrNoConfig},
		"unknown ref":      {func(o *FetchOptions) { o.Ref = "nope" }, nil},
		"wrong token":      {func(o *FetchOptions) { o.Token = "ghs_otherothertokenvalue0123456" }, nil},
		"malformed repo":   {func(o *FetchOptions) { o.Repo = "acme" }, nil},
		"unreachable host": {func(o *FetchOptions) { o.APIBase = "http://127.0.0.1:1" }, nil},
	} {
		opts := fetchOpts(f, dest)
		tc.mutate(&opts)
		_, err := Fetch(context.Background(), opts)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
		for _, secret := range []string{testToken, opts.Token} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error holds the token: %v", name, err)
			}
		}
	}
}

func TestFetchBoundsWhatItDownloads(t *testing.T) {
	t.Run("a config over the size cap", func(t *testing.T) {
		f := newFakeContents(t, map[string]string{"renovate-global/config.json": "{}"})
		f.sizes["renovate-global/config.json"] = maxConfigBytes + 1
		if _, err := Fetch(context.Background(), fetchOpts(f, t.TempDir())); !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("too many tool files", func(t *testing.T) {
		files := map[string]string{"renovate-global/config.json": "{}"}
		for i := range maxToolFiles + 1 {
			files["renovate-global/tools/t"+string(rune('a'+i%26))+string(rune('a'+i/26))] = "x"
		}
		f := newFakeContents(t, files)
		if _, err := Fetch(context.Background(), fetchOpts(f, t.TempDir())); !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("a symlink in the tools directory", func(t *testing.T) {
		f := newFakeContents(t, map[string]string{
			"renovate-global/config.json":   "{}",
			"renovate-global/tools/link.sh": "/etc/passwd",
		})
		f.kinds["renovate-global/tools/link.sh"] = "symlink"
		dest := t.TempDir()
		if _, err := Fetch(context.Background(), fetchOpts(f, dest)); err == nil {
			t.Fatal("a symlink in the tools directory was accepted")
		}
		if _, err := os.Lstat(filepath.Join(dest, "tools", "link.sh")); err == nil {
			t.Error("the symlink was written")
		}
	})
}

func TestFetchRefusesAnEntryNameThatEscapesTheDestination(t *testing.T) {
	for _, bad := range []string{"..", "a/b", `a\b`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": bad, "path": "renovate-global/" + bad, "type": "file"}})
		}))
		dest := t.TempDir()
		_, err := Fetch(context.Background(), FetchOptions{APIBase: srv.URL, Token: testToken, Repo: "acme/renovate", Ref: "main", Dest: dest})
		srv.Close()
		if err == nil {
			t.Errorf("entry name %q accepted", bad)
		}
	}
}

func TestFetchDoesNotFollowARedirectWithTheToken(t *testing.T) {
	var mu sync.Mutex
	var leaked []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		leaked = append(leaked, r.Header.Get("Authorization"))
		mu.Unlock()
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), FetchOptions{APIBase: srv.URL, Token: testToken, Repo: "acme/renovate", Ref: "main", Dest: t.TempDir()}); err == nil {
		t.Fatal("a redirect was treated as success")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(leaked) != 0 {
		t.Errorf("the redirect target was called: %v", leaked)
	}
}

func TestMounted(t *testing.T) {
	t.Run("config and tools", func(t *testing.T) {
		cfgDir, toolsDir := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"a":1}`), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Mounted(cfgDir, toolsDir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Source != SourceMounted || cfg.Path != filepath.Join(cfgDir, "config.json") || cfg.Raw != `{"a":1}` || cfg.ToolsDir != toolsDir {
			t.Errorf("cfg = %+v", cfg)
		}
	})
	t.Run("config without tools", func(t *testing.T) {
		cfgDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(cfgDir, "config.js"), []byte("module.exports={}"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Mounted(cfgDir, filepath.Join(cfgDir, "missing"))
		if err != nil || cfg.ToolsDir != "" {
			t.Errorf("cfg = %+v, err = %v", cfg, err)
		}
	})
	t.Run("nothing mounted", func(t *testing.T) {
		if _, err := Mounted(t.TempDir(), t.TempDir()); !errors.Is(err, ErrNotMounted) {
			t.Errorf("err = %v, want ErrNotMounted", err)
		}
		if _, err := Mounted(filepath.Join(t.TempDir(), "absent"), "/nonexistent"); !errors.Is(err, ErrNotMounted) {
			t.Errorf("err = %v, want ErrNotMounted for a missing directory", err)
		}
	})
	t.Run("a config over the size cap", func(t *testing.T) {
		cfgDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(strings.Repeat("x", maxConfigBytes+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Mounted(cfgDir, ""); !errors.Is(err, ErrTooLarge) {
			t.Errorf("err = %v, want ErrTooLarge", err)
		}
	})
}

func TestWithoutOwnedKeys(t *testing.T) {
	t.Run("a json config loses the keys Release Ops owns and keeps allowedCommands as it is", func(t *testing.T) {
		raw := `{"platform":"gitlab","token":"x","repositories":["a/b"],"allowScripts":true,"allowedCommands":["^a$"],"onboarding":false}`
		cfg := &Config{Path: filepath.Join(t.TempDir(), "config.json"), Raw: raw}
		out := t.TempDir()

		p, removed, err := cfg.WithoutOwnedKeys(out)
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(removed)
		if want := []string{"allowScripts", "platform", "repositories", "token"}; !slices.Equal(removed, want) {
			t.Errorf("removed = %v, want %v", removed, want)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(got, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc) != 2 || doc["onboarding"] != false || doc["allowedCommands"] == nil {
			t.Errorf("rewritten config = %s", got)
		}
		if p == cfg.Path {
			t.Error("the original file path was handed back")
		}
	})
	t.Run("owned keys inside force are removed too and the rest of force is kept", func(t *testing.T) {
		raw := `{"force":{"repositories":["acme/x"],"platform":"gitlab","labels":["a"]}}`
		cfg := &Config{Path: filepath.Join(t.TempDir(), "config.json"), Raw: raw}

		p, removed, err := cfg.WithoutOwnedKeys(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		slices.Sort(removed)
		if want := []string{"force.platform", "force.repositories"}; !slices.Equal(removed, want) {
			t.Errorf("removed = %v, want %v", removed, want)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := `{"force":{"labels":["a"]}}`; string(got) != want {
			t.Errorf("rewritten config = %s, want %s", got, want)
		}
	})
	t.Run("a force that is not an object is left as it is", func(t *testing.T) {
		for _, raw := range []string{`{"force":null}`, `{"force":["repositories"]}`, `{"force":"x"}`} {
			cfg := &Config{Path: "/config/config.json", Raw: raw}
			p, removed, err := cfg.WithoutOwnedKeys(t.TempDir())
			if err != nil || p != cfg.Path || removed != nil {
				t.Errorf("%s: p = %q, removed = %v, err = %v", raw, p, removed, err)
			}
		}
	})
	t.Run("a config without them is left where it is", func(t *testing.T) {
		cfg := &Config{Path: "/config/config.json", Raw: `{"onboarding":false}`}
		p, removed, err := cfg.WithoutOwnedKeys(t.TempDir())
		if err != nil || p != cfg.Path || removed != nil {
			t.Errorf("p = %q, removed = %v, err = %v", p, removed, err)
		}
	})
	t.Run("javascript and unparseable json are handed over as they are", func(t *testing.T) {
		for _, c := range []*Config{
			{Path: "/config/config.js", Raw: "module.exports = { platform: 'gitlab' }"},
			{Path: "/config/config.json", Raw: "{ // comment\n platform: 'x' }"},
		} {
			p, removed, err := c.WithoutOwnedKeys(t.TempDir())
			if err != nil || p != c.Path || removed != nil {
				t.Errorf("%s: p = %q, removed = %v, err = %v", c.Path, p, removed, err)
			}
		}
	})
}
