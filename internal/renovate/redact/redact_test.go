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
		"json escaped": `{"msg":"` + strings.ReplaceAll(secret, "/", `\/`) + `"}`,
		"worker":       "secret=" + secret,
	} {
		out := r.String(in)
		for _, leaked := range []string{token, secret, url.QueryEscape(secret), base64.StdEncoding.EncodeToString([]byte(secret)), base64.RawURLEncoding.EncodeToString([]byte(token))} {
			if strings.Contains(out, leaked) {
				t.Errorf("%s: %q still holds %q", name, out, leaked)
			}
		}
		if name != "json escaped" && !strings.Contains(out, Placeholder) {
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
