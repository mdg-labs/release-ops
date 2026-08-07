package source_test

import (
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestIsValidKindAcceptsAllSourceKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range source.ValidKinds {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			if !source.IsValidKind(kind) {
				t.Fatalf("IsValidKind(%q) = false, want true", kind)
			}
		})
	}
}

func TestIsValidKindRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	if source.IsValidKind("bitbucket") {
		t.Fatal("IsValidKind(bitbucket) = true, want false")
	}
}
