package updateprs

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mdg-labs/release-ops/internal/store"
)

func TestFromReportReadsTheBranchOfThePR(t *testing.T) {
	report := `{"repositories":{"Acme/Widgets":{"branches":[
	  {"branchName":"renovate/other","prNo":7,"sha":"s7","upgrades":[{"depName":"other","currentVersion":"1","newVersion":"2"}]},
	  {"branchName":"renovate/group","prNo":12,"sha":"s12","upgrades":[
	    {"depName":"lodash","manager":"npm","datasource":"npm","currentVersion":"4.17.20","newVersion":"4.17.21","updateType":"patch"},
	    {"packageName":"@types/node","manager":"npm","datasource":"npm","currentValue":"^20.0.0","newValue":"^22.0.0","updateType":"major"},
	    {"depName":"alpine","manager":"dockerfile","datasource":"docker","currentDigest":"sha256:aaa","newDigest":"sha256:bbb","updateType":"pinDigest"},
	    {"depName":"go","manager":"gomod","updateType":"rollback","currentVersion":1.5,"newVersion":2},
	    {"depName":"lodash","manager":"npm","datasource":"npm","currentVersion":"4.17.20","newVersion":"4.17.21","updateType":"patch"},
	    {"manager":"npm","currentVersion":"1","newVersion":"2"},
	    "not an object"
	  ]}]}}}`
	e, ok := fromReport(report, "acme/widgets", 12, "renovate/group")
	if !ok || e.SHA != "s12" {
		t.Fatalf("entry = %+v, %v", e, ok)
	}
	want := []store.UpdatePRDependency{
		{Name: "lodash", Manager: "npm", Datasource: "npm", From: "4.17.20", To: "4.17.21", UpdateType: "patch", Source: "report"},
		{Name: "@types/node", Manager: "npm", Datasource: "npm", From: "^20.0.0", To: "^22.0.0", UpdateType: "major", Source: "report"},
		{Name: "alpine", Manager: "dockerfile", Datasource: "docker", From: "sha256:aaa", To: "sha256:bbb", UpdateType: "digest", Source: "report"},
		{Name: "go", Manager: "gomod", From: "1.5", To: "2", UpdateType: "other", Source: "report"},
	}
	if len(e.Dependencies) != len(want) {
		t.Fatalf("dependencies = %+v", e.Dependencies)
	}
	for i := range want {
		if e.Dependencies[i] != want[i] {
			t.Errorf("dependency %d = %+v, want %+v", i, e.Dependencies[i], want[i])
		}
	}
}

func TestFromReportFallsBackToTheBranchNameOnlyForABranchWithoutAPR(t *testing.T) {
	report := `{"repositories":{"a/b":{"branches":[
	  {"branchName":"renovate/x","prNo":3,"upgrades":[{"depName":"old"}]},
	  {"branchName":"renovate/x","upgrades":[{"depName":"new"}]}]}}}`
	e, ok := fromReport(report, "a/b", 9, "renovate/x")
	if !ok || len(e.Dependencies) != 1 || e.Dependencies[0].Name != "new" {
		t.Fatalf("entry = %+v, %v", e, ok)
	}
	if _, ok := fromReport(`{"repositories":{"a/b":{"branches":[{"branchName":"renovate/x","prNo":3,"upgrades":[{"depName":"old"}]}]}}}`, "a/b", 9, "renovate/x"); ok {
		t.Fatal("a branch of another PR matched by its name")
	}
}

func TestFromReportYieldsNothingForAReportItCannotRead(t *testing.T) {
	for name, report := range map[string]string{
		"not json":             `nope`,
		"no repositories":      `{}`,
		"another repository":   `{"repositories":{"x/y":{"branches":[{"prNo":1,"upgrades":[{"depName":"a"}]}]}}}`,
		"branches not a list":  `{"repositories":{"a/b":{"branches":{"prNo":1}}}}`,
		"upgrades not a list":  `{"repositories":{"a/b":{"branches":[{"prNo":1,"upgrades":{"depName":"a"}}]}}}`,
		"no named upgrade":     `{"repositories":{"a/b":{"branches":[{"prNo":1,"upgrades":[{"manager":"npm"}]}]}}}`,
		"upgrade name a list":  `{"repositories":{"a/b":{"branches":[{"prNo":1,"upgrades":[{"depName":["a"]}]}]}}}`,
		"repository not a map": `{"repositories":[]}`,
	} {
		if e, ok := fromReport(report, "a/b", 1, "renovate/x"); ok {
			t.Errorf("%s: entry = %+v", name, e)
		}
	}
}

const updatesTable = `This PR contains the following updates:

| Package | Type | Update | Change | Pending |
|---|---|---|---|---|
| [lodash](https://lodash.com/) | dependencies | patch | ` + "[`4.17.20` -> `4.17.21`](https://renovatebot.com/diffs/npm/lodash/4.17.20/4.17.21)" + ` | |
| [@types/node](https://example.com) ([source](https://github.com/x/y)) | devDependencies | major | ` + "`^20.0.0` -> `^22.0.0`" + ` | |
| [alpine](https://alpinelinux.org) | final | digest | ` + "`abc1234` -> `def5678`" + ` | |
| plain-name | dependencies | pin | ` + "`1.0` -> `1.0@sha256:x`" + ` | |
| | dependencies | patch | ` + "`1` -> `2`" + ` | |
| [lodash](https://lodash.com/) | dependencies | patch | ` + "[`4.17.20` -> `4.17.21`](https://x)" + ` | |

---

### Configuration
`

func TestBodyDependenciesReadTheUpdatesTable(t *testing.T) {
	got := bodyDependencies(updatesTable)
	want := []store.UpdatePRDependency{
		{Name: "lodash", From: "4.17.20", To: "4.17.21", UpdateType: "patch", Source: "body"},
		{Name: "@types/node", From: "^20.0.0", To: "^22.0.0", UpdateType: "major", Source: "body"},
		{Name: "alpine", From: "abc1234", To: "def5678", UpdateType: "digest", Source: "body"},
		{Name: "plain-name", From: "1.0", To: "1.0@sha256:x", UpdateType: "other", Source: "body"},
	}
	if len(got) != len(want) {
		t.Fatalf("dependencies = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dependency %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBodyDependenciesNeedAPackageAndAChangeColumn(t *testing.T) {
	// The columns of the default Renovate body of a repository with the Mend preset have no
	// Update column: the type stays empty.
	d := bodyDependencies("| Package | Change | Age |\n|---|---|---|\n| [lodash](https://l) | `1` -> `2` | |\n")
	if len(d) != 1 || d[0].Name != "lodash" || d[0].UpdateType != "" || d[0].To != "2" {
		t.Fatalf("dependencies = %+v", d)
	}
	for name, body := range map[string]string{
		"no table":        "Configure Renovate\n\n - [ ] <!-- create-config-migration-pr -->Select.\n",
		"another table":   "| Setting | Value |\n|---|---|\n| [automerge](https://x) | `false` -> `true` |\n",
		"no Change":       "| Package | Update |\n|---|---|\n| [a](https://x) | patch |\n",
		"no separator":    "| Package | Change |\n| [a](https://x) | `1` -> `2` |\n",
		"table after gap": "| Package | Change |\n\n|---|---|\n| [a](https://x) | `1` -> `2` |\n",
	} {
		if d := bodyDependencies(body); len(d) != 0 {
			t.Errorf("%s: dependencies = %+v", name, d)
		}
	}
}

func TestBodyDependenciesAreBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("| Package | Change |\n|---|---|\n")
	for i := range 500 {
		b.WriteString("| [dep" + strings.Repeat("x", i) + "](https://x) | `1` -> `2` | \n")
	}
	if d := bodyDependencies(b.String()); len(d) != maxDependencies {
		t.Fatalf("dependencies = %d, want the cap %d", len(d), maxDependencies)
	}
	long := "| Package | Change |\n|---|---|\n| [" + strings.Repeat("n", 5000) + "](https://x) | `" + strings.Repeat("v", 5000) + "` -> `2` |\n"
	d := bodyDependencies(long)
	if len(d) != 1 || len(d[0].Name) != maxNameBytes || len(d[0].From) != maxFieldBytes {
		t.Fatalf("name and version are not capped: %d, %d", len(d[0].Name), len(d[0].From))
	}
}

func TestReleaseNotesAreTheSectionUpToTheClosingRule(t *testing.T) {
	body := "Updates.\n\n---\n\n### Release Notes\n\n<details>\n<summary>lodash</summary>\n\n#### 4.17.21\n\n- fixed {{ .Secret }} <script>x</script>\n</details>\n\n---\n\n### Configuration\n\nbla\n"
	got := releaseNotes(body)
	want := "<details>\n<summary>lodash</summary>\n\n#### 4.17.21\n\n- fixed {{ .Secret }} <script>x</script>\n</details>"
	if got != want {
		t.Fatalf("release notes = %q, want %q", got, want)
	}
	if got := releaseNotes("### Release Notes\n\nlast section, no rule\r\n"); got != "last section, no rule" {
		t.Fatalf("release notes without a closing rule = %q", got)
	}
	for name, body := range map[string]string{
		"no section":       "Updates.\n\n---\n\n### Configuration\n",
		"an empty section": "### Release Notes\n\n---\n",
		"another heading":  "### Release Notes for x\n\ntext\n---\n",
		"mentioned inline": "See the Release Notes below.\n",
	} {
		if got := releaseNotes(body); got != "" {
			t.Errorf("%s: release notes = %q", name, got)
		}
	}
}

func TestReleaseNotesAreTruncatedAtALineBoundaryWithAnEllipsis(t *testing.T) {
	line := strings.Repeat("é", 99) + "\n" // 100 characters
	body := "### Release Notes\n" + strings.Repeat(line, 60) + "\n---\n"
	got := releaseNotes(body)
	if n := utf8.RuneCountInString(got); n > MaxReleaseNotesChars || !strings.HasSuffix(got, "é…") {
		t.Fatalf("release notes have %d characters and end %q", n, got[len(got)-6:])
	}
	if lines := strings.Split(strings.TrimSuffix(got, "…"), "\n"); len(lines) != 39 || utf8.RuneCountInString(lines[38]) != 99 {
		t.Fatalf("cut inside a line: %d lines", len(lines))
	}
	// One line longer than the limit is cut inside it, the ellipsis counted in.
	got = releaseNotes("### Release Notes\n" + strings.Repeat("x", 9000) + "\n")
	if utf8.RuneCountInString(got) != MaxReleaseNotesChars || !strings.HasSuffix(got, "x…") {
		t.Fatalf("release notes have %d characters", utf8.RuneCountInString(got))
	}
	// Text of exactly the limit is kept as it is.
	exact := strings.Repeat("y", MaxReleaseNotesChars)
	if got := releaseNotes("### Release Notes\n" + exact + "\n"); got != exact {
		t.Fatalf("release notes of the limit were changed: %d characters", utf8.RuneCountInString(got))
	}
}

func TestNormalizeUpdateType(t *testing.T) {
	for in, want := range map[string]string{
		"major": "major", "Minor": "minor", "patch": "patch", "digest": "digest", "pinDigest": "digest",
		"lockFileMaintenance": "lockfile", "lockfileUpdate": "lockfile", "pin": "other", "rollback": "other",
		"bump": "other", "replacement": "other", "": "other", "whatever": "other",
	} {
		if got := normalizeUpdateType(in, true); got != want {
			t.Errorf("normalizeUpdateType(%q) = %q, want %q", in, got, want)
		}
	}
	if got := normalizeUpdateType("", false); got != "" {
		t.Errorf("an absent type from a body = %q, want none", got)
	}
}
