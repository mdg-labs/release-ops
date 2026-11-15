package updatetickets_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

func candidate(automerge *bool, labels []string, types ...string) store.TicketCandidate {
	c := store.TicketCandidate{Automerge: automerge, Labels: labels}
	for _, ty := range types {
		c.Dependencies = append(c.Dependencies, store.UpdatePRDependency{Name: "x", UpdateType: ty})
	}
	return c
}

func TestFilterAllows(t *testing.T) {
	for name, tc := range map[string]struct {
		filter string
		pr     store.TicketCandidate
		want   bool
	}{
		"the default lets a patch through":                    {updatetickets.DefaultFilterJSON, candidate(nil, nil, "patch"), true},
		"the default lets an unknown type through":            {updatetickets.DefaultFilterJSON, candidate(nil, nil, ""), true},
		"the default lets other through":                      {updatetickets.DefaultFilterJSON, candidate(boolp(false), nil, "other"), true},
		"the default skips an automerged PR":                  {updatetickets.DefaultFilterJSON, candidate(boolp(true), nil, "patch"), false},
		"an unknown automerge counts as not automerged":       {`{"skipAutomerged":true}`, candidate(nil, nil, "patch"), true},
		"skipAutomerged off lets an automerged PR through":    {`{"skipAutomerged":false}`, candidate(boolp(true), nil, "patch"), true},
		"nothing stored is the default":                       {``, candidate(boolp(true), nil, "patch"), false},
		"a type list passes a listed type":                    {`{"updateTypes":["major","minor"]}`, candidate(nil, nil, "minor"), true},
		"a type list passes when one dependency matches":      {`{"updateTypes":["major"]}`, candidate(nil, nil, "patch", "major"), true},
		"a type list refuses an unlisted type":                {`{"updateTypes":["major"]}`, candidate(nil, nil, "patch"), false},
		"a type list refuses other":                           {`{"updateTypes":["major","minor","patch","digest","lockfile"]}`, candidate(nil, nil, "other"), false},
		"a type list refuses an unknown type":                 {`{"updateTypes":["major"]}`, candidate(nil, nil, ""), false},
		"a label list passes a carried label":                 {`{"labels":["deps","security"]}`, candidate(nil, []string{"x", "security"}, "patch"), true},
		"a label list refuses a PR without one":               {`{"labels":["deps"]}`, candidate(nil, []string{"Deps"}, "patch"), false},
		"a label list refuses a PR without labels":            {`{"labels":["deps"]}`, candidate(nil, nil, "patch"), false},
		"every key must pass":                                 {`{"updateTypes":["major"],"labels":["deps"]}`, candidate(nil, []string{"deps"}, "patch"), false},
		"every key passes":                                    {`{"updateTypes":["major"],"labels":["deps"]}`, candidate(nil, []string{"deps"}, "major"), true},
		"an automerged PR is refused whatever else it passes": {`{"updateTypes":["major"],"labels":["deps"]}`, candidate(boolp(true), []string{"deps"}, "major"), false},
	} {
		f, err := updatetickets.ParseFilter(tc.filter)
		if err != nil {
			t.Fatalf("%s: ParseFilter: %v", name, err)
		}
		if got := f.Allows(tc.pr); got != tc.want {
			t.Errorf("%s: Allows = %v, want %v", name, got, tc.want)
		}
	}
}

func TestParseFilterRefusesAShapeItCannotRead(t *testing.T) {
	if _, err := updatetickets.ParseFilter(`{"labels":"deps"}`); err == nil {
		t.Fatal("a string for labels must not parse: a filter that cannot be read must not let every PR through")
	}
}

func TestValidateFilter(t *testing.T) {
	ok := map[string]string{
		`{}`: `{"updateTypes":[],"labels":[],"skipAutomerged":true}`,
		`{"updateTypes":["major","lockfile"],"labels":["deps"],"skipAutomerged":false}`: `{"updateTypes":["major","lockfile"],"labels":["deps"],"skipAutomerged":false}`,
		`{"skipAutomerged":false}`: `{"updateTypes":[],"labels":[],"skipAutomerged":false}`,
	}
	for in, want := range ok {
		got, err := updatetickets.ValidateFilter(json.RawMessage(in))
		if err != nil || got != want {
			t.Errorf("ValidateFilter(%s) = %s, %v, want %s", in, got, err, want)
		}
	}
	for in, want := range map[string]string{
		`{"updateTypes":["other"]}`: "updateTypes",
		`{"updateTypes":["Major"]}`: "updateTypes",
		`{"updateTypes":"major"}`:   "updateTypes",
		`{"updateTypes":null}`:      "updateTypes",
		`{"labels":[1]}`:            "labels",
		`{"labels":"deps"}`:         "labels",
		`{"skipAutomerged":"yes"}`:  "skipAutomerged",
		`{"skipAutomerged":null}`:   "skipAutomerged",
		`{"automerge":true}`:        "automerge",
		`[]`:                        "JSON object",
		`"x"`:                       "JSON object",
		`null`:                      "JSON object",
	} {
		if _, err := updatetickets.ValidateFilter(json.RawMessage(in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateFilter(%s) = %v, want an error naming %q", in, err, want)
		}
	}
}
