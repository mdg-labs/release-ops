package redact

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

const token = "ghs_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"

func TestStringRemovesKnownSecretsInEveryEncoding(t *testing.T) {
	secret := "w0rker/secret+with=chars&more"
	r := New(token, secret)

	for name, in := range map[string]string{
		"plain":        "token " + token + " end",
		"in a url":     "https://x-access-token:" + token + "@github.com/acme/repo.git",
		"url escaped":  "q=" + url.QueryEscape(secret),
		"base64":       "basic " + base64.StdEncoding.EncodeToString([]byte(secret)),
		"raw base64":   base64.RawURLEncoding.EncodeToString([]byte(token)),
		"json escaped": `{"msg":"w0rker\/secret+with=chars&more"}`,
		"json html":    `{"msg":"w0rker/secret+with=chars\u0026more"}`,
		"json both":    `{"msg":"w0rker\/secret+with=chars\u0026more"}`,
		"worker":       "secret=" + secret,
	} {
		out := r.String(in)
		for _, leaked := range []string{token, secret, "w0rker", "secret+with", "chars", url.QueryEscape(secret), base64.StdEncoding.EncodeToString([]byte(secret)), base64.RawURLEncoding.EncodeToString([]byte(token))} {
			if strings.Contains(out, leaked) {
				t.Errorf("%s: %q still holds %q", name, out, leaked)
			}
		}
		if !strings.Contains(out, Placeholder) {
			t.Errorf("%s: %q has no placeholder", name, out)
		}
	}
}

func TestStringRemovesSecretShapedText(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----"
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxMjM0NSJ9.c2lnbmF0dXJlLXZhbHVl"
	hexKey := strings.Repeat("ab12", 16)

	for name, tc := range map[string]struct{ in, leaked string }{
		"installation token": {"auth ghs_" + strings.Repeat("A1", 20), "ghs_A1"},
		"personal token":     {"ghp_" + strings.Repeat("z", 36), "ghp_z"},
		"fine grained":       {"github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz", "github_pat_11"},
		"pem":                {"key " + pem + " tail", "MIIEow"},
		"cut pem":            {`{"k":"-----BEGIN PRIVATE KEY-----\nMIIEowIBAAKCAQEA`, "MIIEow"},
		"jwt":                {"jwt " + jwt, "eyJhbGci"},
		"hex key":            {"key=" + hexKey, hexKey},
		"url credentials":    {"clone https://user:hunter2hunter2@example.com/r.git", "hunter2"},
		"bearer":             {"Authorization: Bearer abcdefghijklmnop1234", "abcdefghijklmnop"},
		"env dump":           {"APP_ENCRYPTION_KEY=short-but-secret", "short-but-secret"},
		"worker secret env":  {"WORKER_SECRET=0123456789abcdef", "0123456789abcdef"},
	} {
		if out := Shapes(tc.in); strings.Contains(out, tc.leaked) || !strings.Contains(out, Placeholder) {
			t.Errorf("%s: %q -> %q", name, tc.in, out)
		}
	}
}

func TestStringLeavesOrdinaryTextAlone(t *testing.T) {
	for _, in := range []string{
		`{"level":30,"msg":"Repository finished","repository":"acme/repo","branch":"renovate/lodash-4.x"}`,
		"commit 4b825dc642cb6eb9a060e54bf8d69288fbee4904 on main",
		"https://github.com/acme/repo/pull/12",
		"",
	} {
		if out := New(token).String(in); out != in {
			t.Errorf("%q changed to %q", in, out)
		}
	}
}

func TestNewIgnoresEmptyAndShortSecrets(t *testing.T) {
	in := "the word abc and nothing else"
	if out := New("", "abc").String(in); out != in {
		t.Errorf("a short secret shredded the text: %q", out)
	}
}

func TestStringConsumesTheWholeURLUserInfo(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"an @ in the password":     {"clone https://user:p@ss@word@example.com/repo now", "clone https://" + Placeholder + "@example.com/repo now"},
		"a plain user-info":        {"https://user:pw@host/x", "https://" + Placeholder + "@host/x"},
		"no user-info":             {"https://host/x", "https://host/x"},
		"an address after a path":  {"see https://host/x and mail dev@example.com", "see https://host/x and mail dev@example.com"},
		"an address after a space": {"https://host dev@example.com", "https://host dev@example.com"},
		"an @ in the query":        {"https://host/x?mail=a@b.example", "https://host/x?mail=a@b.example"},
		"a # in the password":      {"https://user:p#ss@host/x", "https://" + Placeholder + "@host/x"},
		"a # in a token password":  {"https://x-access-token:ab#cd@github.com/o/r.git", "https://" + Placeholder + "@github.com/o/r.git"},
		"a ? in the password":      {"https://user:p?ss@host/x", "https://" + Placeholder + "@host/x"},
		"an @ in the fragment":     {"https://host#a@b", "https://" + Placeholder + "@b"},
		"a quoted url":             {`"https://user:p@ss@host/x"`, `"https://` + Placeholder + `@host/x"`},
	} {
		if got := Shapes(tc.in); got != tc.want {
			t.Errorf("%s: %q -> %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

func TestStringRedactsQuotedAndEscapedAssignmentValues(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"double quoted":      {`PASSWORD="correct horse battery staple" NEXT=1`, "PASSWORD=" + Placeholder + " NEXT=1"},
		"escaped quote":      {`PASSWORD="a \" b c" NEXT=1`, "PASSWORD=" + Placeholder + " NEXT=1"},
		"single quoted":      {`TOKEN='a b c' NEXT=1`, "TOKEN=" + Placeholder + " NEXT=1"},
		"escaped spaces":     {`API_TOKEN=abc\ def ghi`, "API_TOKEN=" + Placeholder + " ghi"},
		"unquoted":           {"APP_ENCRYPTION_KEY=short-but-secret rest", "APP_ENCRYPTION_KEY=" + Placeholder + " rest"},
		"next assignment":    {"SECRET=abc OTHER_TOKEN=def", "SECRET=" + Placeholder + " OTHER_TOKEN=" + Placeholder},
		"not a secret name":  {`PATH="/usr/bin"`, `PATH="/usr/bin"`},
		"not a secret quote": {`NAME='a b c' MODE="x y"`, `NAME='a b c' MODE="x y"`},
	} {
		if got := Shapes(tc.in); got != tc.want {
			t.Errorf("%s: %q -> %q, want %q", name, tc.in, got, tc.want)
		}
	}
}
