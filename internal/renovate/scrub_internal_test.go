package renovate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const boundsToken = "ghs_TokenForTheScrubBoundsTest0123456789abcd"

func plantGit(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["config"] = "[core]\n\tbare = false\n"
	for name, content := range files {
		path := filepath.Join(dir, ".git", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func withLimits(t *testing.T, l scrubLimits) {
	t.Helper()
	old := gitLimits
	gitLimits = l
	t.Cleanup(func() { gitLimits = old })
}

// A scrub that exceeds a bound fails, so the copy is removed rather than handed on unchecked.
func TestTheScrubFailsClosedBeyondItsBounds(t *testing.T) {
	for name, tc := range map[string]struct {
		limits scrubLimits
		files  map[string]string
	}{
		"too many entries": {
			limits: scrubLimits{configBytes: 1 << 20, fileBytes: 1 << 20, entries: 5, depth: 10},
			files:  map[string]string{"a": "", "b": "", "c": "", "d": "", "e": "", "f": ""},
		},
		"too deep": {
			limits: scrubLimits{configBytes: 1 << 20, fileBytes: 1 << 20, entries: 100, depth: 2},
			files:  map[string]string{"modules/a/modules/b/config": ""},
		},
		"a config too large": {
			limits: scrubLimits{configBytes: 16, fileBytes: 1 << 20, entries: 100, depth: 10},
			files:  map[string]string{"modules/a/config": strings.Repeat("#", 64)},
		},
		"a file too large to search": {
			limits: scrubLimits{configBytes: 1 << 20, fileBytes: 16, entries: 100, depth: 10},
			files:  map[string]string{"big": strings.Repeat("x", 64)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withLimits(t, tc.limits)
			dir := plantGit(t, tc.files)
			if err := scrubGitDir(dir, boundsToken); err == nil {
				t.Error("the scrub reported a clean copy")
			}
		})
	}

	t.Run("within the bounds", func(t *testing.T) {
		dir := plantGit(t, map[string]string{"a": "x", "modules/a/config": "[core]\n"})
		if err := scrubGitDir(dir, boundsToken); err != nil {
			t.Error(err)
		}
	})
}

// The search reads a file in chunks: a token that straddles two of them is still found.
func TestTheSearchFindsATokenAcrossChunkBoundaries(t *testing.T) {
	for offset := searchChunk - len(boundsToken) - 2; offset <= searchChunk+2; offset++ {
		body := strings.Repeat("x", offset) + boundsToken + strings.Repeat("y", 100)
		dir := plantGit(t, map[string]string{"big": body})
		if err := scrubGitDir(dir, boundsToken); err == nil {
			t.Fatalf("the token at offset %d was not found", offset)
		}
	}
	dir := plantGit(t, map[string]string{"big": strings.Repeat("x", 3*searchChunk)})
	if err := scrubGitDir(dir, boundsToken); err != nil {
		t.Errorf("a file without the token failed: %v", err)
	}
}

// Only a git dir's own object store goes unread; a directory that is merely named objects, such
// as a submodule's git dir, is searched like any other.
func TestOnlyAGitDirsObjectStoreIsNotSearched(t *testing.T) {
	withLimits(t, scrubLimits{configBytes: 1 << 20, fileBytes: 32, entries: 100, depth: 10})
	big := strings.Repeat("x", 64)

	dir := plantGit(t, map[string]string{"HEAD": "ref: refs/heads/main\n", "objects/pack/pack-1.pack": big})
	if err := scrubGitDir(dir, boundsToken); err != nil {
		t.Errorf("a file in the object store was searched: %v", err)
	}
	dir = plantGit(t, map[string]string{"modules/a/HEAD": "ref: refs/heads/main\n", "modules/a/objects/pack/pack-1.pack": big})
	if err := scrubGitDir(dir, boundsToken); err != nil {
		t.Errorf("a file in a submodule's object store was searched: %v", err)
	}
	for name, files := range map[string]map[string]string{
		"a submodule named objects":        {"modules/objects/big": big},
		"a submodule named a/objects":      {"modules/a/objects/big": big},
		"a nested submodule named objects": {"modules/a/modules/objects/big": big},
		"an objects dir that is no store":  {"HEAD": "x", "modules/objects/HEAD": "x", "modules/objects/refs/big": big},
	} {
		t.Run(name, func(t *testing.T) {
			if err := scrubGitDir(plantGit(t, files), boundsToken); err == nil {
				t.Error("a file outside an object store was not searched")
			}
		})
	}
}
