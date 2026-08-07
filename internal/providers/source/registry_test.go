package source_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestRegistryGetReturnsProviderByKind(t *testing.T) {
	t.Parallel()

	registry := source.NewRegistry()
	mock := &source.MockSourceProvider{
		Release: &source.Release{
			Tag:         "v1.0.0",
			URL:         "https://example.com/releases/v1.0.0",
			PublishedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		},
	}
	if err := registry.Register(source.KindGitHub, mock); err != nil {
		t.Fatalf("Register: %v", err)
	}

	provider, err := registry.Get(source.KindGitHub)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release.Tag != "v1.0.0" {
		t.Fatalf("tag = %q, want v1.0.0", release.Tag)
	}
}

func TestRegistryGetUnknownKindReturnsError(t *testing.T) {
	t.Parallel()

	registry := source.NewRegistry()

	_, err := registry.Get("bitbucket")
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !errors.Is(err, source.ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}
}

func TestRegistryGetUnregisteredValidKindReturnsError(t *testing.T) {
	t.Parallel()

	registry := source.NewRegistry()

	_, err := registry.Get(source.KindGitLab)
	if err == nil {
		t.Fatal("expected error for unregistered kind")
	}
	if !errors.Is(err, source.ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}
}

func TestRegistryRegisterRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	registry := source.NewRegistry()
	err := registry.Register("invalid", &source.MockSourceProvider{})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !errors.Is(err, source.ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}
}

func TestMockSourceProviderRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	mock := &source.MockSourceProvider{
		Release: &source.Release{Tag: "v1.0.0"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mock.GetLatestRelease(ctx, "owner/repo")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
