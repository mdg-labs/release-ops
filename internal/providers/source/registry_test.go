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

	release, err := provider.GetLatestRelease(context.Background(), "owner/repo", source.ReleaseOptions{})
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

	_, err := mock.GetLatestRelease(ctx, "owner/repo", source.ReleaseOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestMockSourceProviderReturnsConfiguredError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("provider unavailable")
	mock := &source.MockSourceProvider{Err: wantErr}

	_, err := mock.GetLatestRelease(context.Background(), "owner/repo", source.ReleaseOptions{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestRegistryRegisterRejectsNilProvider(t *testing.T) {
	t.Parallel()

	registry := source.NewRegistry()
	err := registry.Register(source.KindGitHub, nil)
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestRegistryAllSourceKindsRoundTrip(t *testing.T) {
	t.Parallel()

	release := &source.Release{
		Tag:         "v1.0.0",
		URL:         "https://example.com/releases/v1.0.0",
		PublishedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
	}

	for _, kind := range source.ValidKinds {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			registry := source.NewRegistry()
			mock := &source.MockSourceProvider{Release: release}
			if err := registry.Register(kind, mock); err != nil {
				t.Fatalf("Register(%q): %v", kind, err)
			}

			provider, err := registry.Get(kind)
			if err != nil {
				t.Fatalf("Get(%q): %v", kind, err)
			}

			got, err := provider.GetLatestRelease(context.Background(), "owner/repo", source.ReleaseOptions{})
			if err != nil {
				t.Fatalf("GetLatestRelease: %v", err)
			}
			if got.Tag != release.Tag {
				t.Fatalf("tag = %q, want %q", got.Tag, release.Tag)
			}
		})
	}
}
