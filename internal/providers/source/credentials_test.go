package source_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestParseTokenSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		secret  []byte
		want    string
		wantErr bool
	}{
		{name: "token", secret: []byte(`{"token":" ghp_x "}`), want: "ghp_x"},
		{name: "empty token", secret: []byte(`{"token":""}`), want: ""},
		{name: "empty payload", secret: []byte{}, want: ""},
		{name: "nil payload", secret: nil, want: ""},
		{name: "whitespace payload", secret: []byte("  \n"), want: ""},
		{name: "malformed", secret: []byte(`{"token"`), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := source.ParseTokenSecret(tc.secret)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("token = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmptyPayloadPollsGitHubWithoutAuthorization(t *testing.T) {
	t.Parallel()

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v1.0.0",
			"html_url":     "https://github.com/acme/widget/releases/tag/v1.0.0",
			"published_at": "2026-08-07T10:00:00Z",
		})
	}))
	t.Cleanup(server.Close)

	token, err := source.ParseTokenSecret([]byte{})
	if err != nil {
		t.Fatalf("ParseTokenSecret: %v", err)
	}
	provider := source.NewGitHubSource(token, newHostRewritingClient(server, "api.github.com"))
	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release == nil || release.Tag != "v1.0.0" {
		t.Fatalf("release = %+v, want v1.0.0", release)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization = %q, want none", gotAuth)
	}
}
