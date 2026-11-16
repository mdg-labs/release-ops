package updateprs

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// maxBodyBytes bounds how much of a PR body is read; GitHub itself caps a body at 65,536
	// characters.
	maxBodyBytes = 256 << 10
	// MaxReleaseNotesChars is the length of the stored release-notes excerpt, the ellipsis
	// included.
	MaxReleaseNotesChars = 4000
)

// bodyDependencies reads the updates table of a Renovate PR body: the rows of a table whose
// header has the columns Package and Change, and Update when it is there. The name is the link
// text of Package, the versions are the two code spans of Change joined by "->", and the type
// is the Update cell. Renovate's columns are configurable; a table without those two columns,
// and a row without a name, yield nothing.
func bodyDependencies(body string) []store.UpdatePRDependency {
	body = capBytes(body, maxBodyBytes)
	var deps []store.UpdatePRDependency
	var pkg, change, update int
	inTable, sawSeparator := false, false
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(line, "|") {
			inTable, sawSeparator = false, false
			continue
		}
		cells := splitRow(line)
		switch {
		case !inTable:
			inTable, sawSeparator = true, false
			pkg, change, update = columnIndex(cells, "package"), columnIndex(cells, "change"), columnIndex(cells, "update")
		case !sawSeparator:
			sawSeparator = isSeparator(cells)
			if !sawSeparator {
				inTable = false
			}
		case pkg >= 0 && change >= 0 && len(cells) > max(pkg, change, update):
			name := capBytes(packageName(cells[pkg]), maxNameBytes)
			if name == "" {
				continue
			}
			from, to := versions(cells[change])
			d := store.UpdatePRDependency{
				Name: name, From: capBytes(from, maxFieldBytes), To: capBytes(to, maxFieldBytes),
				Source: store.DependencySourceBody,
			}
			if update >= 0 {
				d.UpdateType = normalizeUpdateType(strings.Trim(strings.TrimSpace(cells[update]), "`"), false)
			}
			deps = addDependency(deps, d)
		}
	}
	return deps
}

// splitRow splits a markdown table row into its trimmed cells; \| does not split.
func splitRow(line string) []string {
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func columnIndex(header []string, name string) int {
	for i, h := range header {
		if strings.EqualFold(strings.Trim(h, "*_ "), name) {
			return i
		}
	}
	return -1
}

var separatorCell = regexp.MustCompile(`^:?-{1,}:?$`)

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if !separatorCell.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
}

var (
	linkText = regexp.MustCompile(`^\[([^\]]+)\]`)
	codeSpan = regexp.MustCompile("`([^`]*)`")
)

// packageName is the link text of a Package cell, or the cell's text up to a parenthesis when
// it is not a link.
func packageName(cell string) string {
	if m := linkText.FindStringSubmatch(cell); m != nil {
		return strings.TrimSpace(strings.Trim(m[1], "`"))
	}
	name, _, _ := strings.Cut(cell, " (")
	return strings.TrimSpace(strings.Trim(name, "`"))
}

// versions reads "`from` -> `to`" out of a Change cell; empty when the cell has no such pair.
func versions(cell string) (from, to string) {
	spans := codeSpan.FindAllStringSubmatch(cell, -1)
	if len(spans) < 2 || !strings.Contains(cell, "->") {
		return "", ""
	}
	return strings.TrimSpace(spans[0][1]), strings.TrimSpace(spans[1][1])
}

var releaseNotesHeading = regexp.MustCompile(`(?i)^#{1,6}\s+Release Notes\s*$`)

// releaseNotes returns the release-notes section of a Renovate PR body, truncated to
// MaxReleaseNotesChars: the text after the "Release Notes" heading up to the closing "---"
// rule. It is empty when the body has no such section or the section has no text.
func releaseNotes(body string) string {
	body = capBytes(body, maxBodyBytes)
	var section []string
	inSection := false
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case !inSection:
			inSection = releaseNotesHeading.MatchString(strings.TrimSpace(line))
		case strings.TrimSpace(line) == "---":
			return truncateNotes(strings.Join(section, "\n"))
		default:
			section = append(section, line)
		}
	}
	return truncateNotes(strings.Join(section, "\n"))
}

// truncateNotes trims the text and, when it is longer than MaxReleaseNotesChars, cuts it at the
// last line boundary that leaves room for the ellipsis that ends it, or inside the last line
// when the text has no earlier boundary.
func truncateNotes(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= MaxReleaseNotesChars {
		return s
	}
	budget := []rune(s)[:MaxReleaseNotesChars-1]
	cut := string(budget)
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \t\r\n") + "…"
}
