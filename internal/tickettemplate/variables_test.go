package tickettemplate_test

import (
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
