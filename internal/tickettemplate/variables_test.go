package tickettemplate_test

import (
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/tickettemplate"
)

func validate(tmpl string) error {
	return tickettemplate.NewRenderer("kaneo", tickettemplate.ContentTemplates{Description: tmpl}).Validate()
}

func TestValidateChecksWhatTheDotIsInsideWithAndRange(t *testing.T) {
	t.Parallel()

	for name, tmpl := range map[string]string{
		"a field of the list inside a with":       "{{with .Dependencies}}{{.Name}}{{end}}",
		"a field of a string inside a with":       "{{with .Repository}}{{.Foo}}{{end}}",
		"a field of the release notes in a with":  "{{with .ReleaseNotes}}{{.Len}}{{end}}",
		"a missing field in a range over a list":  "{{range .Dependencies}}{{.Foo}}{{end}}",
		"a missing field in a range over the dot": "{{with .Dependencies}}{{range .}}{{.Foo}}{{end}}{{end}}",
	} {
		if err := validate(tmpl); err == nil {
			t.Errorf("%s: Validate() accepted %q", name, tmpl)
		}
	}
	for name, tmpl := range map[string]string{
		"a field of each dependency in a range":  "{{range .Dependencies}}{{.Name}}{{end}}",
		"the dot of a with over a string":        "{{with .Repository}}{{.}}{{end}}",
		"the dot of a with over the list":        "{{with .Dependencies}}{{len .}}{{end}}",
		"a range over the dot of a with":         "{{with .Dependencies}}{{range .}}{{.Name}}{{end}}{{end}}",
		"a field of the pull request in a with":  "{{with .PullRequest}}{{.Title}}{{end}}",
		"the else of a with keeps the outer dot": "{{with .Repository}}{{.}}{{else}}{{.PullRequest.URL}}{{end}}",
	} {
		if err := validate(tmpl); err != nil {
			t.Errorf("%s: Validate() = %v for %q", name, err, tmpl)
		}
	}
}

func TestValidateChecksTheFieldsChainedOntoAnExpression(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ tmpl, want string }{
		"a missing field of a parenthesised field":       {"{{(.PullRequest).Foo}}", ".Foo"},
		"a variable of the release contract":             {"{{(.Release).Tag}}", ".Dependencies and .ReleaseNotes"},
		"a missing field below the pull request of a PR": {"{{(.Supersede.NewPullRequest).Branch}}", ".Branch"},
		"a missing field inside the parenthesised base":  {"{{(.Foo).Title}}", ".Foo"},
		"a missing field of the dot":                     {"{{with .PullRequest}}{{(.).Foo}}{{end}}", ".Foo"},
		"a chain inside a branch not taken":              {"{{if false}}{{(.PullRequest).Foo}}{{end}}", ".Foo"},
	} {
		err := validate(c.tmpl)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Validate(%q) = %v, want an error naming %q", name, c.tmpl, err, c.want)
		}
	}
	for name, tmpl := range map[string]string{
		"a field of a parenthesised field":     "{{(.PullRequest).Title}}",
		"a field below a parenthesised field":  "{{(.Supersede.NewPullRequest).URL}}",
		"a field of a parenthesised dot":       "{{with .PullRequest}}{{(.).Title}}{{end}}",
		"a base that is not tracked":           "{{(index .Dependencies 0).Name}}",
		"a chain inside a range over the list": "{{range .Dependencies}}{{(.).Name}}{{end}}",
	} {
		if err := validate(tmpl); err != nil {
			t.Errorf("%s: Validate() = %v for %q", name, err, tmpl)
		}
	}
}

func TestValidateChecksFieldsOfVariablesAgainstWhatTheyAreBoundTo(t *testing.T) {
	t.Parallel()

	for name, tmpl := range map[string]string{
		"an alias of the pull request":              "{{$pr := .PullRequest}}{{$pr.Foo}}",
		"the element of a range":                    "{{range $d := .Dependencies}}{{$d.Foo}}{{end}}",
		"the element of a range with an index":      "{{range $i, $d := .Dependencies}}{{$d.Foo}}{{end}}",
		"an alias of the list":                      "{{$deps := .Dependencies}}{{$deps.Name}}",
		"an alias of a string":                      "{{$r := .Repository}}{{$r.Owner}}",
		"an alias of a nested field":                "{{$n := .Supersede.NewPullRequest}}{{$n.Branch}}",
		"a variable of a with":                      "{{with $pr := .PullRequest}}{{$pr.Foo}}{{end}}",
		"a variable of an if":                       "{{if $pr := .PullRequest}}{{$pr.Foo}}{{end}}",
		"an alias used inside a range":              "{{$pr := .PullRequest}}{{range .Dependencies}}{{$pr.Foo}}{{end}}",
		"an alias rebound by an assignment":         "{{$x := .PullRequest}}{{$x = .Supersede}}{{$x.Title}}",
		"an alias of a variable":                    "{{$a := .PullRequest}}{{$b := $a}}{{$b.Foo}}",
		"a field of the release contract":           "{{$pr := .PullRequest}}{{$pr.Release.Tag}}",
		"the element of a range over an alias":      "{{$deps := .Dependencies}}{{range $d := $deps}}{{$d.Foo}}{{end}}",
		"an alias used in a branch not taken":       "{{$pr := .PullRequest}}{{if false}}{{$pr.Foo}}{{end}}",
		"an alias declared in the enclosing branch": "{{if true}}{{$pr := .PullRequest}}{{if true}}{{$pr.Foo}}{{end}}{{end}}",
	} {
		if err := validate(tmpl); err == nil {
			t.Errorf("%s: Validate() accepted %q", name, tmpl)
		}
	}
	for name, tmpl := range map[string]string{
		"an alias of the pull request":             "{{$pr := .PullRequest}}{{$pr.Title}} {{$pr.URL}}",
		"the element of a range":                   "{{range $d := .Dependencies}}{{$d.Name}} {{$d.UpdateType}}{{end}}",
		"the element of a range with an index":     "{{range $i, $d := .Dependencies}}{{$i}} {{$d.To}}{{end}}",
		"the root through $":                       "{{range .Dependencies}}{{$.PullRequest.Title}}{{end}}",
		"a variable of a with":                     "{{with $pr := .PullRequest}}{{$pr.Title}} {{.Number}}{{end}}",
		"an alias used inside a range":             "{{$pr := .PullRequest}}{{range .Dependencies}}{{$pr.Title}}{{.Name}}{{end}}",
		"an alias rebound to another pull request": "{{$x := .PullRequest}}{{$x = .Supersede.NewPullRequest}}{{$x.Title}}",
		"a variable that is not tracked":           "{{$x := len .Dependencies}}{{$x}}",
		"a variable of a function call":            "{{$x := printf \"%s\" .Repository}}{{$x.Whatever}}",
		"a name reused in a nested scope":          "{{$x := .Repository}}{{range .Dependencies}}{{$x := .}}{{$x.Name}}{{end}}{{$x}}",
		"a variable assigned in a branch":          "{{$x := .PullRequest}}{{if true}}{{$x = .Supersede}}{{end}}{{$x.Whatever}}",
		"a variable of a with in the else":         "{{with $r := .Repository}}{{$r}}{{else}}{{$r}}{{end}}",
	} {
		if err := validate(tmpl); err != nil {
			t.Errorf("%s: Validate() = %v for %q", name, err, tmpl)
		}
	}
}

func TestValidateChecksTheTemplatesAnInvocationNames(t *testing.T) {
	t.Parallel()

	for name, tmpl := range map[string]string{
		"a missing field in a definition":                 `{{define "x"}}{{.Foo}}{{end}}{{template "x" .}}`,
		"a field of the release contract in a definition": `{{define "x"}}{{.Release.Tag}}{{end}}{{template "x" .}}`,
		"a definition invoked with a field":               `{{define "x"}}{{.Foo}}{{end}}{{template "x" .PullRequest}}`,
		"a definition invoked from inside a range":        `{{define "x"}}{{.Foo}}{{end}}{{range .Dependencies}}{{template "x" .}}{{end}}`,
		"a definition that reaches the root through $":    `{{define "x"}}{{$.Foo}}{{end}}{{template "x" .}}`,
		"a definition invoked by another definition":      `{{define "x"}}{{template "y" .}}{{end}}{{define "y"}}{{.Foo}}{{end}}{{template "x" .}}`,
		"a definition invoked with the pull request":      `{{define "x"}}{{.Dependencies}}{{end}}{{template "x" .PullRequest}}`,
		"a block":                          `{{block "x" .}}{{.Foo}}{{end}}`,
		"a definition that invokes itself": `{{define "x"}}{{.Foo}}{{template "x" .}}{{end}}{{template "x" .}}`,
	} {
		if err := validate(tmpl); err == nil {
			t.Errorf("%s: Validate() accepted %q", name, tmpl)
		}
	}
	for name, tmpl := range map[string]string{
		"a definition of fields the root has":             `{{define "x"}}{{.PullRequest.Title}}{{end}}{{template "x" .}}`,
		"a definition invoked with the pull request":      `{{define "x"}}{{.Title}}{{end}}{{template "x" .PullRequest}}`,
		"a definition invoked for each dependency":        `{{define "x"}}{{.Name}}{{end}}{{range .Dependencies}}{{template "x" .}}{{end}}`,
		"a definition invoked without an argument":        `{{define "x"}}{{.Whatever}}{{end}}{{template "x"}}`,
		"a definition that is never invoked":              `{{define "x"}}{{.Foo}}{{end}}ok`,
		"a definition that invokes itself":                `{{define "x"}}{{.Name}}{{if false}}{{template "x" .}}{{end}}{{end}}{{range .Dependencies}}{{template "x" .}}{{end}}`,
		"definitions that invoke each other":              `{{define "a"}}{{.Name}}{{template "b" .}}{{end}}{{define "b"}}{{.To}}{{template "a" .}}{{end}}{{range .Dependencies}}{{template "a" .}}{{end}}`,
		"a definition that reaches the root through $":    `{{define "x"}}{{$.Title}}{{end}}{{template "x" .PullRequest}}`,
		"an invocation that is not tracked":               `{{define "x"}}{{.Whatever}}{{end}}{{template "x" len .Dependencies}}`,
		"an invocation of a template that is not defined": `{{template "missing" .}}`,
	} {
		if err := validate(tmpl); err != nil {
			t.Errorf("%s: Validate() = %v for %q", name, err, tmpl)
		}
	}
}
