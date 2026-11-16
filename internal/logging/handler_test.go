package logging_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
)

// secretKinds are the values the app and the worker hold or mint. None has the shape of a token
// or key, so a test that passes shows the registry removed it, not the shape rules.
var secretKinds = map[string]string{
	"app private key":     "-----BEGIN RSA PRIVATE KEY-----\nopaque-key-body-line-one\nopaque-key-body-line-two\n-----END RSA PRIVATE KEY-----\n",
	"client secret":       "opaque-client-secret-7f3a",
	"webhook secret":      "opaque-webhook-secret-91bd",
	"setup token":         "opaque-setup-token-c04e",
	"worker secret":       "opaque-worker-secret-2d68",
	"installation token":  "opaque-installation-token-55aa",
	"user token":          "opaque-user-token-b1c9",
	"oauth code":          "opaque-oauth-code-e832",
	"oauth state":         "opaque-oauth-state-4f07",
	"session token":       "opaque-session-token-a6d1",
	"integration payload": `{"apiKey":"opaque-integration-key-3b9e/with+chars"}`,
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newLogger(reg *logging.Registry, json bool) (*slog.Logger, *lockedBuffer) {
	out := &lockedBuffer{}
	var inner slog.Handler = slog.NewTextHandler(out, nil)
	if json {
		inner = slog.NewJSONHandler(out, nil)
	}
	return slog.New(logging.NewHandler(inner, reg)), out
}

func registryWithEveryKind() *logging.Registry {
	reg := logging.NewRegistry(nil)
	for _, v := range secretKinds {
		reg.Add(v)
	}
	return reg
}

type structWithSecret struct {
	Name  string
	Token string
}

type panickyError struct{}

func (panickyError) Error() string { panic("no text") }

type valuer struct{ secret string }

func (v valuer) LogValue() slog.Value { return slog.GroupValue(slog.String("inner", "held "+v.secret)) }

// Every secret kind, in each place a log line can hold it and in each encoding a log handler
// writes it, never reaches the output.
func TestEverySecretKindIsRemovedFromEveryPlaceInARecord(t *testing.T) {
	places := map[string]func(l *slog.Logger, secret string){
		"message":          func(l *slog.Logger, s string) { l.Info("startup: using " + s) },
		"string attribute": func(l *slog.Logger, s string) { l.Info("m", "value", s) },
		"attribute key":    func(l *slog.Logger, s string) { l.Info("m", s, "value") },
		"error":            func(l *slog.Logger, s string) { l.Error("m", "err", fmt.Errorf("github said: %s", s)) },
		"wrapped error": func(l *slog.Logger, s string) {
			l.Error("m", "err", fmt.Errorf("outer: %w", errors.Join(errors.New("a"), fmt.Errorf("body %q", s))))
		},
		"struct":            func(l *slog.Logger, s string) { l.Info("m", "obj", structWithSecret{Name: "n", Token: s}) },
		"pointer to struct": func(l *slog.Logger, s string) { l.Info("m", "obj", &structWithSecret{Token: s}) },
		"byte slice":        func(l *slog.Logger, s string) { l.Info("m", "raw", []byte(s)) },
		"string slice":      func(l *slog.Logger, s string) { l.Info("m", "list", []string{"a", s}) },
		"map":               func(l *slog.Logger, s string) { l.Info("m", "map", map[string]string{"k": s}) },
		"group":             func(l *slog.Logger, s string) { l.Info("m", slog.Group("g", slog.String("v", s))) },
		"nested group": func(l *slog.Logger, s string) {
			l.Info("m", slog.Group("g", slog.Group("h", slog.Any("e", errors.New(s)))))
		},
		"log valuer":    func(l *slog.Logger, s string) { l.Info("m", "v", valuer{s}) },
		"with attrs":    func(l *slog.Logger, s string) { l.With("ctx", s).Info("m") },
		"with group":    func(l *slog.Logger, s string) { l.WithGroup("g").With("ctx", s).Info("m", "x", 1) },
		"url user info": func(l *slog.Logger, s string) { l.Info("m", "url", "https://user:"+s+"@example.com/path") },
		"quoted env":    func(l *slog.Logger, s string) { l.Info("m", "env", `WORKER_SECRET="`+s+`"`) },
		"standard log":  nil,
	}
	for _, jsonOut := range []bool{false, true} {
		for kind, secret := range secretKinds {
			for place, emit := range places {
				name := fmt.Sprintf("json=%v/%s/%s", jsonOut, kind, place)
				t.Run(name, func(t *testing.T) {
					l, out := newLogger(registryWithEveryKind(), jsonOut)
					if emit == nil {
						log.New(slogWriter{l}, "", 0).Print("standard log line with " + secret)
					} else {
						emit(l, secret)
					}
					assertAbsent(t, out.String(), kind, secret)
					if out.String() == "" {
						t.Fatal("nothing was logged")
					}
				})
			}
		}
	}
}

// slogWriter sends what the standard logger writes to l, as slog.SetDefault does.
type slogWriter struct{ l *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	w.l.Info(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func assertAbsent(t *testing.T, out, kind, secret string) {
	t.Helper()
	forms := []string{secret}
	for _, line := range strings.Split(secret, "\n") {
		if len(line) > 12 && !strings.HasPrefix(line, "-----") {
			forms = append(forms, line)
		}
	}
	for _, f := range forms {
		if strings.Contains(out, f) {
			t.Errorf("the %s (%.20q...) is in the output:\n%s", kind, f, out)
		}
	}
	if !strings.Contains(out, logging.Placeholder) {
		t.Errorf("the output holds no redaction marker:\n%s", out)
	}
}

// A secret registered after a handler was derived with attributes is removed from the
// attributes that were added before it.
func TestASecretRegisteredAfterWithAttrsIsStillRemoved(t *testing.T) {
	reg := logging.NewRegistry(nil)
	l, out := newLogger(reg, false)
	child := l.With("request_token", "late-secret-value-1").WithGroup("g")
	reg.Add("late-secret-value-1")
	child.Info("m")
	if strings.Contains(out.String(), "late-secret-value-1") {
		t.Fatalf("a secret registered after With is in the output: %s", out.String())
	}
}

// A secret registered after a handler was derived with a group is removed from the group's name.
func TestASecretRegisteredAfterWithGroupIsRemovedFromTheGroupName(t *testing.T) {
	const secret = "late-group-secret-value-2"
	for name, json := range map[string]bool{"text": false, "json": true} {
		t.Run(name, func(t *testing.T) {
			reg := logging.NewRegistry(nil)
			l, out := newLogger(reg, json)
			child := l.WithGroup(secret)
			reg.Add(secret)
			child.Info("m", "k", "v")
			if strings.Contains(out.String(), secret) {
				t.Fatalf("a secret registered after WithGroup is in the output: %s", out.String())
			}
			if !strings.Contains(out.String(), logging.Placeholder) {
				t.Fatalf("the group name is not replaced by the placeholder: %s", out.String())
			}
		})
	}
}

func TestCredentialHeadersAreMaskedByName(t *testing.T) {
	l, out := newLogger(logging.NewRegistry(nil), false)
	l.Info("request",
		"Authorization", "token plain-scheme-value",
		"cookie", "release_ops_session=opaque-cookie-value",
		"x_hub_signature_256", "sha256=opaque",
		slog.Group("headers", slog.String("Set-Cookie", "a=b-opaque-cookie")),
		"headers", http.Header{
			"Authorization":       {"Basic opaque-basic-value"},
			"X-Hub-Signature-256": {"sha256=notahexvalue"},
			"X-GitHub-Event":      {"push"},
		},
	)
	for _, leaked := range []string{"plain-scheme-value", "opaque-cookie-value", "sha256=opaque", "opaque-basic-value", "notahexvalue", "b-opaque-cookie"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("%q is in the output: %s", leaked, out.String())
		}
	}
	if !strings.Contains(out.String(), "push") {
		t.Errorf("a header that is no credential was removed: %s", out.String())
	}
}

func TestHeaderLinesInTextAreMaskedToTheEndOfTheLine(t *testing.T) {
	l, out := newLogger(logging.NewRegistry(nil), false)
	l.Error("upstream rejected the request:\nAuthorization: token two words here\nCookie: a=b; c=d\nX-Hub-Signature-256: sha256=opaque-signature\nOK: kept")
	l.Error("m", "dump", `{"authorization":"token abc def","x":"kept"}`)
	for _, leaked := range []string{"two words here", "a=b", "opaque-signature", "abc def"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("%q is in the output: %s", leaked, out.String())
		}
	}
	if !strings.Contains(out.String(), "OK: kept") {
		t.Errorf("a line that is no credential header was removed: %s", out.String())
	}
}

func TestShapesAreRemovedWithoutRegistration(t *testing.T) {
	l, out := newLogger(logging.NewRegistry(nil), false)
	l.Info("a ghs_0123456789abcdefghij token and https://u:p@host/x", "err", errors.New("Bearer abcdefghijkl"))
	for _, leaked := range []string{"ghs_0123456789abcdefghij", "u:p@", "abcdefghijkl"} {
		if strings.Contains(out.String(), leaked) {
			t.Errorf("%q is in the output: %s", leaked, out.String())
		}
	}
}

// A line that is not secret is written as it was, with its level, time and attributes.
func TestOtherValuesPassThroughUnchanged(t *testing.T) {
	l, out := newLogger(logging.NewRegistry(nil), true)
	d := 1500 * time.Millisecond
	l.Warn("request", "status", 200, "ok", true, "took", d, "ratio", 0.5, "when", time.Date(2026, 11, 15, 1, 2, 3, 0, time.UTC), "nothing", nil)
	for _, want := range []string{`"level":"WARN"`, `"msg":"request"`, `"status":200`, `"ok":true`, `"took":1500000000`, `"ratio":0.5`, `2026-11-15T01:02:03Z`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %s: %s", want, out.String())
		}
	}
}

func TestAnErrorWhoseTextPanicsDoesNotPanicTheCaller(t *testing.T) {
	l, out := newLogger(logging.NewRegistry(nil), false)
	l.Error("m", "err", panickyError{})
	if !strings.Contains(out.String(), "m") || strings.Contains(out.String(), "no text") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestEnabledFollowsTheWrappedHandler(t *testing.T) {
	h := logging.NewHandler(slog.NewTextHandler(&lockedBuffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}), nil)
	if h.Enabled(context.Background(), slog.LevelInfo) || !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatal("Enabled does not follow the wrapped handler's level")
	}
}

// The setup token is shown in the record that prints it and in no other line, and not in a
// later record that repeats it.
func TestPrintOnceShowsTheSecretInThatRecordOnly(t *testing.T) {
	out := &lockedBuffer{}
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	reg := logging.NewRegistry(nil)
	slog.SetDefault(slog.New(logging.NewHandler(slog.NewTextHandler(out, nil), reg)))

	const token = "opaque-one-time-token-for-print-once-0001"
	// PrintOnce registers in the default registry; the handler under test reads reg, so the
	// follow-up registration is repeated here.
	logging.PrintOnce(context.Background(), token, "setup required: open https://x/setup and enter this token: "+token)
	reg.Add(token)
	slog.Info("later line " + token)
	slog.Error("later", "err", errors.New(token))

	if got := strings.Count(out.String(), token); got != 1 {
		t.Fatalf("token printed %d times, want once:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "enter this token: "+token) {
		t.Fatalf("the printing record does not show the token:\n%s", out.String())
	}
}

// The reveal covers the secret it names: other secrets in the same message are still removed.
func TestPrintOnceStillRemovesOtherSecretsFromTheMessage(t *testing.T) {
	out := &lockedBuffer{}
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	reg := logging.NewRegistry(nil)
	reg.Add("opaque-other-secret-value")
	slog.SetDefault(slog.New(logging.NewHandler(slog.NewTextHandler(out, nil), reg)))

	logging.PrintOnce(context.Background(), "opaque-shown-secret-value", "a opaque-other-secret-value b opaque-shown-secret-value c opaque-other-secret-value")
	if strings.Contains(out.String(), "opaque-other-secret-value") || strings.Count(out.String(), "opaque-shown-secret-value") != 1 {
		t.Fatalf("output = %s", out.String())
	}
}

func TestInstallRoutesTheStandardLogThroughTheRedactingHandler(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	out := &lockedBuffer{}
	logging.Install(out)
	logging.Register("opaque-install-secret-value")

	slog.Info("slog line opaque-install-secret-value")
	log.Printf("standard line opaque-install-secret-value")
	if strings.Contains(out.String(), "opaque-install-secret-value") || strings.Count(out.String(), logging.Placeholder) != 2 {
		t.Fatalf("output = %s", out.String())
	}
}

func TestRedactCoversTextWrittenOutsideTheLogger(t *testing.T) {
	logging.Register("opaque-redact-secret-value")
	if got := logging.Redact("config: opaque-redact-secret-value is wrong"); strings.Contains(got, "opaque-redact-secret-value") {
		t.Fatalf("Redact = %q", got)
	}
}
