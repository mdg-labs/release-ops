package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

// singleReadIntegrations answers Credentials with one revision of an integration and Get and
// DecryptPayload with another, so a Target that read the row in two steps would mix them.
type singleReadIntegrations struct {
	store.IntegrationRepository
	credentialsCalls int
	otherReads       int
}

func (f *singleReadIntegrations) Credentials(context.Context, string) (*store.IntegrationCredentials, error) {
	f.credentialsCalls++
	url := "https://kaneo.example.com"
	return &store.IntegrationCredentials{Kind: "kaneo", BaseURL: &url, Payload: []byte(`{"api_key":"k"}`)}, nil
}

func (f *singleReadIntegrations) Get(context.Context, string) (*store.Integration, error) {
	f.otherReads++
	url := "https://old.example.com"
	return &store.Integration{Kind: "linear", BaseURL: &url}, nil
}

func (f *singleReadIntegrations) DecryptPayload(context.Context, string) ([]byte, error) {
	f.otherReads++
	return []byte(`{"api_key":"old"}`), nil
}

func TestTargetReadsTheIntegrationOnce(t *testing.T) {
	fake := &singleReadIntegrations{}
	target, err := (integrationTargets{integrations: fake, client: http.DefaultClient}).Target(context.Background(), "integ-1")
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	if fake.credentialsCalls != 1 || fake.otherReads != 0 {
		t.Fatalf("Credentials calls = %d, Get/DecryptPayload calls = %d; want 1 and 0", fake.credentialsCalls, fake.otherReads)
	}
	if target.Kind != "kaneo" {
		t.Fatalf("Kind = %q, want the kind of the single read (kaneo)", target.Kind)
	}
}
