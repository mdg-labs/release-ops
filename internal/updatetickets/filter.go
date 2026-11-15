package updatetickets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mdg-labs/release-ops/internal/store"
)

// DefaultFilterJSON is the filter of a project that configures none: every update type, no
// label restriction, automerged PRs skipped (specs.html section 4.3).
const DefaultFilterJSON = `{"updateTypes":[],"labels":[],"skipAutomerged":true}`

// filterUpdateTypes are the update types a filter may list (specs.html section 5.2). A
// dependency of type other, or with no type, passes an empty list only.
var filterUpdateTypes = []string{"major", "minor", "patch", "digest", "lockfile"}

// Filter decides which update PRs get a ticket in a ticket project.
type Filter struct {
	// UpdateTypes empty lets every type through; otherwise a PR needs a dependency of one of them.
	UpdateTypes []string `json:"updateTypes"`
	// Labels empty is no restriction; otherwise a PR needs one of them.
	Labels []string `json:"labels"`
	// SkipAutomerged skips a PR whose automerge is known to be set.
	SkipAutomerged bool `json:"skipAutomerged"`
}

// ParseFilter reads a stored filter; a key it lacks has its default.
func ParseFilter(raw string) (Filter, error) {
	f := Filter{SkipAutomerged: true}
	if raw == "" {
		return f, nil
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return Filter{}, fmt.Errorf("parse filter: %w", err)
	}
	return f, nil
}

// ValidateFilter checks a filter an admin sends and returns it as it is stored, with every key
// present: updateTypes and labels are arrays of strings, updateTypes lists only the five types of
// section 5.2, and skipAutomerged is a boolean. An omitted key has its default.
func ValidateFilter(raw json.RawMessage) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "", errors.New("filter must be a JSON object")
	}
	f := Filter{UpdateTypes: []string{}, Labels: []string{}, SkipAutomerged: true}
	for key, value := range obj {
		var err error
		switch {
		case key != "updateTypes" && key != "labels" && key != "skipAutomerged":
			return "", fmt.Errorf("filter.%s is not a filter key", key)
		case bytes.Equal(bytes.TrimSpace(value), []byte("null")):
			err = errors.New("null")
		case key == "updateTypes":
			err = json.Unmarshal(value, &f.UpdateTypes)
		case key == "labels":
			err = json.Unmarshal(value, &f.Labels)
		default:
			err = json.Unmarshal(value, &f.SkipAutomerged)
		}
		if err != nil {
			return "", fmt.Errorf("filter.%s has the wrong type", key)
		}
	}
	for _, t := range f.UpdateTypes {
		if !slices.Contains(filterUpdateTypes, t) {
			return "", fmt.Errorf("filter.updateTypes must list only major, minor, patch, digest or lockfile, not %q", t)
		}
	}
	out, err := json.Marshal(f)
	if err != nil {
		return "", errors.New("filter must be valid JSON")
	}
	return string(out), nil
}

// Allows reports whether the PR passes the filter. An unknown automerge counts as not
// automerged, and a dependency whose type is unknown matches no type list.
func (f Filter) Allows(c store.TicketCandidate) bool {
	if f.SkipAutomerged && c.Automerge != nil && *c.Automerge {
		return false
	}
	if len(f.Labels) > 0 && !slices.ContainsFunc(c.Labels, func(l string) bool { return slices.Contains(f.Labels, l) }) {
		return false
	}
	if len(f.UpdateTypes) > 0 && !slices.ContainsFunc(c.Dependencies, func(d store.UpdatePRDependency) bool {
		return slices.Contains(filterUpdateTypes, d.UpdateType) && slices.Contains(f.UpdateTypes, d.UpdateType)
	}) {
		return false
	}
	return true
}
