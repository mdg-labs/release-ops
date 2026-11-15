package updateprs

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mdg-labs/release-ops/internal/store"
)

// Caps on what is taken from a report or a PR body. The text is data: it is stored and shown,
// never executed or used as template source.
const (
	maxDependencies = 200
	maxNameBytes    = 512
	maxFieldBytes   = 256
)

// reportBranch is the part of a Renovate report branch entry that is read. The report's schema
// is experimental, so every field is read leniently: a field of an unexpected type is absent.
type reportBranch struct {
	BranchName lenient   `json:"branchName"`
	PrNo       lenient   `json:"prNo"`
	SHA        lenient   `json:"sha"`
	Upgrades   []lenient `json:"upgrades"`
}

type reportUpgrade struct {
	DepName        lenient `json:"depName"`
	PackageName    lenient `json:"packageName"`
	Manager        lenient `json:"manager"`
	Datasource     lenient `json:"datasource"`
	CurrentVersion lenient `json:"currentVersion"`
	CurrentValue   lenient `json:"currentValue"`
	CurrentDigest  lenient `json:"currentDigest"`
	NewVersion     lenient `json:"newVersion"`
	NewValue       lenient `json:"newValue"`
	NewDigest      lenient `json:"newDigest"`
	UpdateType     lenient `json:"updateType"`
}

// lenient reads a JSON string or number as text and anything else as empty. It keeps the raw
// value of an object so a branch or upgrade entry decodes on its own.
type lenient struct {
	text string
	raw  json.RawMessage
}

func (l *lenient) UnmarshalJSON(b []byte) error {
	l.raw = bytes.Clone(b)
	switch {
	case len(b) > 0 && b[0] == '"':
		var s string
		if json.Unmarshal(b, &s) == nil {
			l.text = s
		}
	case len(b) > 0 && (b[0] == '-' || (b[0] >= '0' && b[0] <= '9')):
		var n json.Number
		if json.Unmarshal(b, &n) == nil {
			l.text = n.String()
		}
	}
	return nil
}

// reportEntry is what the report says about one PR.
type reportEntry struct {
	// SHA is the branch's head commit as of the run; empty when the report has none.
	SHA          string
	Dependencies []store.UpdatePRDependency
}

// fromReport finds the entry of the PR in a Renovate report: the branch of
// repositories[fullName].branches whose prNo is the PR's number or, when the branch names no
// PR, whose branchName is the PR's head branch. It reports false when the report is not
// readable, names no such branch or the branch lists no dependency with a name.
func fromReport(report, fullName string, number int64, headRef string) (reportEntry, bool) {
	var doc struct {
		Repositories map[string]struct {
			Branches []json.RawMessage `json:"branches"`
		} `json:"repositories"`
	}
	if json.Unmarshal([]byte(report), &doc) != nil {
		return reportEntry{}, false
	}
	var branches []json.RawMessage
	for name, repo := range doc.Repositories {
		if strings.EqualFold(name, fullName) {
			branches = repo.Branches
			break
		}
	}
	var byBranch *reportBranch
	for _, raw := range branches {
		var b reportBranch
		if json.Unmarshal(raw, &b) != nil {
			continue
		}
		if no, err := strconv.ParseInt(b.PrNo.text, 10, 64); err == nil && no > 0 {
			if no == number {
				return branchEntry(b)
			}
			continue
		}
		if byBranch == nil && headRef != "" && b.BranchName.text == headRef {
			byBranch = &b
		}
	}
	if byBranch == nil {
		return reportEntry{}, false
	}
	return branchEntry(*byBranch)
}

func branchEntry(b reportBranch) (reportEntry, bool) {
	e := reportEntry{SHA: capBytes(b.SHA.text, maxFieldBytes)}
	for _, raw := range b.Upgrades {
		var u reportUpgrade
		if json.Unmarshal(raw.raw, &u) != nil {
			continue
		}
		name := first(u.DepName.text, u.PackageName.text)
		if name == "" {
			continue
		}
		e.Dependencies = addDependency(e.Dependencies, store.UpdatePRDependency{
			Name:       capBytes(name, maxNameBytes),
			Manager:    capBytes(u.Manager.text, maxFieldBytes),
			Datasource: capBytes(u.Datasource.text, maxFieldBytes),
			From:       capBytes(first(u.CurrentVersion.text, u.CurrentValue.text, u.CurrentDigest.text), maxFieldBytes),
			To:         capBytes(first(u.NewVersion.text, u.NewValue.text, u.NewDigest.text), maxFieldBytes),
			UpdateType: normalizeUpdateType(u.UpdateType.text, true),
			Source:     store.DependencySourceReport,
		})
	}
	return e, len(e.Dependencies) > 0
}

// addDependency appends d unless an identical one is there already, or the list is full: a
// monorepo reports the same update once per package file.
func addDependency(list []store.UpdatePRDependency, d store.UpdatePRDependency) []store.UpdatePRDependency {
	if len(list) >= maxDependencies {
		return list
	}
	for _, have := range list {
		if have == d {
			return list
		}
	}
	return append(list, d)
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizeUpdateType maps Renovate's update type to the stored set: major, minor and patch as
// they are, digest and pinDigest to digest, lockFileMaintenance and lockfileUpdate to lockfile,
// and anything else to other. An absent type is other when absentIsOther, and empty (stored as
// NULL) otherwise.
func normalizeUpdateType(t string, absentIsOther bool) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "major":
		return "major"
	case "minor":
		return "minor"
	case "patch":
		return "patch"
	case "digest", "pindigest":
		return "digest"
	case "lockfilemaintenance", "lockfileupdate":
		return "lockfile"
	case "":
		if !absentIsOther {
			return ""
		}
	}
	return "other"
}
