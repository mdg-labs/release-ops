package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/logging"
)

func newOAuthFixture(t *testing.T, handler http.HandlerFunc) (*OAuthClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewOAuthClient(NewClient(srv.URL, 0), srv.URL), srv
}

func TestAuthorizeURLCarriesOnlyTheFlowParameters(t *testing.T) {
	t.Parallel()
	o := NewOAuthClient(NewClient("https://api.github.com", 0), "https://github.com/")
	u, err := url.Parse(o.AuthorizeURL("Iv1.abc", "https://x.example/cb", "st", "ch"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Host != "github.com" || u.Path != "/login/oauth/authorize" {
		t.Errorf("authorize URL = %s%s", u.Host, u.Path)
	}
	want := url.Values{"client_id": {"Iv1.abc"}, "redirect_uri": {"https://x.example/cb"}, "state": {"st"}, "code_challenge": {"ch"}, "code_challenge_method": {"S256"}}
	if fmt.Sprint(u.Query()) != fmt.Sprint(want) {
		t.Errorf("query = %v, want %v", u.Query(), want)
	}
}

func TestExchangeCodeAnswers(t *testing.T) {
	t.Parallel()
	exchange := func(t *testing.T, o *OAuthClient, code string) (UserToken, error) {
		t.Helper()
		return o.ExchangeCode(context.Background(), ExchangeParams{ClientID: "id", ClientSecret: "secret", Code: code, RedirectURI: "https://x.example/cb", CodeVerifier: "v"})
	}

	t.Run("token", func(t *testing.T) {
		t.Parallel()
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/login/oauth/access_token" || r.Header.Get("Accept") != "application/json" {
				t.Errorf("request = %s %s accept %q", r.Method, r.URL.Path, r.Header.Get("Accept"))
			}
			_, _ = w.Write([]byte(`{"access_token":"tok-123","token_type":"bearer","expires_in":28800,"refresh_token":"r"}`))
		})
		tok, err := exchange(t, o, "abc")
		if err != nil || tok.value != "tok-123" {
			t.Fatalf("ExchangeCode = %v, %v", tok, err)
		}
		for _, rendered := range []string{tok.String(), tok.GoString(), fmt.Sprintf("%v %+v %#v", tok, tok, tok), slog.GroupValue(slog.Any("t", tok)).String()} {
			if strings.Contains(rendered, "tok-123") {
				t.Errorf("the token renders its value: %s", rendered)
			}
		}
	})
	t.Run("200 with an error is a rejection", func(t *testing.T) {
		t.Parallel()
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"text"}`))
		})
		if _, err := exchange(t, o, "abc"); !errors.Is(err, ErrOAuthRejected) || strings.Contains(err.Error(), "text") {
			t.Fatalf("err = %v, want ErrOAuthRejected without GitHub's text", err)
		}
	})
	t.Run("server error and unusable answers", func(t *testing.T) {
		t.Parallel()
		for name, h := range map[string]http.HandlerFunc{
			"500":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
			"not json": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
			"no token": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) },
		} {
			o, _ := newOAuthFixture(t, h)
			if _, err := exchange(t, o, "abc"); !errors.Is(err, ErrRequestFailed) {
				t.Errorf("%s: err = %v, want ErrRequestFailed", name, err)
			}
		}
	})
	t.Run("a redirect is not followed", func(t *testing.T) {
		t.Parallel()
		var followed atomic.Int32
		other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed.Add(1) }))
		t.Cleanup(other.Close)
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
		})
		if _, err := exchange(t, o, "abc"); !errors.Is(err, ErrRequestFailed) || followed.Load() != 0 {
			t.Fatalf("err = %v, followed = %d; want a failure and no request to the redirect target", err, followed.Load())
		}
	})
	t.Run("a code that is not a code is refused without a request", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		o, _ := newOAuthFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
		if _, err := exchange(t, o, "a b/../c"); !errors.Is(err, ErrOAuthRejected) || calls.Load() != 0 {
			t.Fatalf("err = %v, calls = %d", err, calls.Load())
		}
	})
}

func TestUserAndOrganizationOwner(t *testing.T) {
	t.Parallel()
	t.Run("user", func(t *testing.T) {
		t.Parallel()
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer tok" {
				t.Errorf("request %s with %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"id":583231,"login":"octocat","type":"User"}`))
		})
		u, err := o.User(context.Background(), UserToken{value: "tok"})
		if err != nil || u.ID != 583231 || u.Login != "octocat" {
			t.Fatalf("User = %+v, %v", u, err)
		}
	})
	t.Run("managed user", func(t *testing.T) {
		t.Parallel()
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":583232,"login":"mona-cat_octo","type":"User"}`))
		})
		u, err := o.User(context.Background(), UserToken{value: "tok"})
		if err != nil || u.ID != 583232 || u.Login != "mona-cat_octo" {
			t.Fatalf("User = %+v, %v; want the managed user to sign in", u, err)
		}
	})
	t.Run("rejected and malformed", func(t *testing.T) {
		t.Parallel()
		o, _ := newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
		if _, err := o.User(context.Background(), UserToken{value: "t"}); !errors.Is(err, ErrUserTokenRejected) {
			t.Errorf("401: err = %v, want ErrUserTokenRejected", err)
		}
		for _, body := range []string{`{}`, `{"id":0,"login":"x"}`, `{"id":5,"login":"bad login"}`, `nope`,
			`{"id":5,"login":"mona/cat"}`, `{"id":5,"login":"_mona"}`, `{"id":5,"login":"mona_"}`, `{"id":5,"login":"mona_oc_to"}`,
			`{"id":5,"login":"mo--na"}`, `{"id":5,"login":"mona/../cat_octo"}`} {
			o, _ := newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := o.User(context.Background(), UserToken{value: "t"}); !errors.Is(err, ErrRequestFailed) {
				t.Errorf("body %q: err = %v, want ErrRequestFailed", body, err)
			}
		}
	})
	t.Run("organization owner", func(t *testing.T) {
		t.Parallel()
		for body, want := range map[string]bool{
			`{"state":"active","role":"admin"}`:  true,
			`{"state":"active","role":"member"}`: false,
			`{"state":"pending","role":"admin"}`: false,
		} {
			o, _ := newOAuthFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/user/memberships/orgs/acme" {
					t.Errorf("path = %s", r.URL.Path)
				}
				_, _ = w.Write([]byte(body))
			})
			got, err := o.IsOrganizationOwner(context.Background(), UserToken{value: "t"}, "acme")
			if err != nil || got != want {
				t.Errorf("%s: owner = %v, %v; want %v", body, got, err, want)
			}
		}
		var calls atomic.Int32
		o, _ := newOAuthFixture(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
		if got, err := o.IsOrganizationOwner(context.Background(), UserToken{value: "t"}, "acme_corp"); err != nil || got || calls.Load() != 0 {
			t.Errorf("an organization name with an underscore: owner = %v, %v, calls = %d; want false without a request", got, err, calls.Load())
		}
		o, _ = newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		if got, err := o.IsOrganizationOwner(context.Background(), UserToken{value: "t"}, "acme"); err != nil || got {
			t.Errorf("404: owner = %v, %v; want false, nil", got, err)
		}
		o, _ = newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		if _, err := o.IsOrganizationOwner(context.Background(), UserToken{value: "t"}, "acme"); !errors.Is(err, ErrRequestFailed) {
			t.Errorf("500: err = %v, want ErrRequestFailed", err)
		}
	})
}

func TestValidUserLogin(t *testing.T) {
	t.Parallel()
	for _, login := range []string{"octocat", "a", "mona-cat", "mona-cat_octo", strings.Repeat("a", 39), "mona_" + strings.Repeat("b", 39)} {
		if !ValidUserLogin(login) {
			t.Errorf("ValidUserLogin(%q) = false, want true", login)
		}
	}
	for _, login := range []string{"", "mona/cat", "_mona", "mona_", "mona--cat", "-mona", "mona-", "mona-_octo", "mona_octo_cat", "mona cat", "@mona", strings.Repeat("a", 40), "mona_" + strings.Repeat("b", 40)} {
		if ValidUserLogin(login) {
			t.Errorf("ValidUserLogin(%q) = true, want false", login)
		}
	}
}

func TestExchangeCodeRegistersOnlyAcceptedCodes(t *testing.T) {
	t.Parallel()
	o, _ := newOAuthFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
	})
	const (
		badCode      = "not a valid code: opaque-malformed-code-0001"
		badVerifier  = "opaque-verifier-of-a-malformed-code-0002"
		goodCode     = "opaque-accepted-code-0003"
		goodVerifier = "opaque-verifier-of-an-accepted-code-0004"
	)
	exchange := func(code, verifier string) {
		t.Helper()
		_, err := o.ExchangeCode(context.Background(), ExchangeParams{ClientID: "id", ClientSecret: "secret", Code: code, RedirectURI: "https://x.example/cb", CodeVerifier: verifier})
		if !errors.Is(err, ErrOAuthRejected) {
			t.Fatalf("ExchangeCode(%q) = %v, want ErrOAuthRejected", code, err)
		}
	}

	exchange(badCode, badVerifier)
	for _, v := range []string{badCode, badVerifier} {
		if got := logging.Redact("line " + v); got != "line "+v {
			t.Errorf("a value of a rejected code is registered: %q", got)
		}
	}

	exchange(goodCode, goodVerifier)
	for _, v := range []string{goodCode, goodVerifier} {
		if got := logging.Redact("line " + v); strings.Contains(got, v) {
			t.Errorf("%s of an accepted code is not removed from log text: %q", v, got)
		}
	}
}
