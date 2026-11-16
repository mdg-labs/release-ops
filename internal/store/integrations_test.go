package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/store"
)

func TestIntegrationCredentialsComeFromOneRow(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	oldURL, newURL := "https://old.example.com", "https://new.example.com"
	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind: "kaneo", Name: "Kaneo", BaseURL: &oldURL, Secret: []byte(`{"api_key":"old-key"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Integrations().Credentials(ctx, created.ID)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if got.Kind != "kaneo" || got.BaseURL == nil || *got.BaseURL != oldURL {
		t.Fatalf("Credentials = %+v, want kaneo at %s", got, oldURL)
	}
	if key, _ := ticket.ParseKaneoSecret(got.Payload); key != "old-key" {
		t.Fatalf("key = %q, want old-key", key)
	}

	if _, err := s.Integrations().Update(ctx, created.ID, store.UpdateIntegrationInput{
		Name: "Kaneo", BaseURL: &newURL, Secret: []byte(`{"api_key":"new-key"}`),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = s.Integrations().Credentials(ctx, created.ID)
	if err != nil {
		t.Fatalf("Credentials after update: %v", err)
	}
	if got.BaseURL == nil || *got.BaseURL != newURL {
		t.Fatalf("BaseURL = %v, want %s", got.BaseURL, newURL)
	}
	if key, _ := ticket.ParseKaneoSecret(got.Payload); key != "new-key" {
		t.Fatalf("key = %q, want new-key", key)
	}

	if _, err := s.Integrations().Credentials(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Credentials of a missing integration: err = %v, want sql.ErrNoRows", err)
	}
}

func TestIntegrationCredentialsWithoutSecretHaveEmptyPayload(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "linear", Name: "Linear"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Integrations().Credentials(ctx, created.ID)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if got.Kind != "linear" || len(got.Payload) != 0 {
		t.Fatalf("Credentials = %+v, want kind linear and an empty payload", got)
	}
}
