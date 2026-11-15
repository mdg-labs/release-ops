// Package redact removes secrets from text that is stored, shown or logged: the literal values
// a caller knows (the job's installation token, the worker secret) and anything shaped like a
// token or a key. It depends on nothing else of Release Ops, so the worker's Renovate handler
// and the app's raw global-config display use the same one.
package redact

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Placeholder replaces every secret.
const Placeholder = "[REDACTED]"

// minSecretLen is the shortest literal that is replaced: a shorter one would shred ordinary
// text, and no real token or key is that short.
const minSecretLen = 6

var shapes = []struct {
	re   *regexp.Regexp
	repl string
}{
	// GitHub tokens: installation (ghs_), user (ghu_), OAuth (gho_), refresh (ghr_), personal
	// (ghp_) and fine-grained personal access tokens.
	{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`), Placeholder},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{16,}`), Placeholder},
	// A PEM private key through its end marker; one whose end was cut off goes to the end of
	// its line and the base64 lines that follow.
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), Placeholder},
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[^\n"]*(?:\n[A-Za-z0-9+/=]{16,})*`), Placeholder},
	// A JSON Web Token.
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), Placeholder},
	// A 256-bit key as hex, the shape of APP_ENCRYPTION_KEY.
	{regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`), Placeholder},
	// Credentials in a URL's user-info part.
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s"'@]+@`), "${1}" + Placeholder + "@"},
	// An Authorization header value.
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "${1} " + Placeholder},
	// An environment dump or a log line of NAME=value for a secret-named variable.
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PRIVATE_KEY|ENCRYPTION_KEY)[A-Z0-9_]*)=[^\s"']+`), "${1}=" + Placeholder},
}

// Redactor replaces known secrets and secret-shaped text.
type Redactor struct {
	literals *strings.Replacer
}

// New returns a Redactor for the given secrets. Each is replaced as it is and in the encodings
// it travels in: URL-escaped, base64 and JSON-escaped. Empty and very short values are ignored.
func New(secrets ...string) *Redactor {
	var forms []string
	for _, s := range secrets {
		if len(s) < minSecretLen {
			continue
		}
		forms = append(forms, s, url.QueryEscape(s), url.PathEscape(s), jsonEscape(s))
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			forms = append(forms, enc.EncodeToString([]byte(s)))
		}
	}
	slices.Sort(forms)
	forms = slices.Compact(forms)
	// The replacer tries its arguments in order at each position, so the longest comes first.
	slices.SortStableFunc(forms, func(a, b string) int { return len(b) - len(a) })

	r := &Redactor{}
	if len(forms) > 0 {
		pairs := make([]string, 0, 2*len(forms))
		for _, f := range forms {
			pairs = append(pairs, f, Placeholder)
		}
		r.literals = strings.NewReplacer(pairs...)
	}
	return r
}

func jsonEscape(s string) string {
	raw, err := json.Marshal(s)
	if err != nil || len(raw) < 2 {
		return s
	}
	return string(raw[1 : len(raw)-1])
}

// String returns s with every known secret and every secret-shaped part replaced.
func (r *Redactor) String(s string) string {
	if r.literals != nil {
		s = r.literals.Replace(s)
	}
	for _, shape := range shapes {
		s = shape.re.ReplaceAllString(s, shape.repl)
	}
	return s
}

// Shapes redacts only what is shaped like a secret, for a caller that knows no literal.
func Shapes(s string) string {
	return New().String(s)
}
