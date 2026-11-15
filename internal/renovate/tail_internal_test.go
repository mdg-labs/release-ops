package renovate

import (
	"strings"
	"testing"
)

func TestTailBufferString(t *testing.T) {
	t.Run("nothing dropped keeps everything", func(t *testing.T) {
		b := newTailBuffer(100)
		_, _ = b.Write([]byte("first\nsecond"))
		if got := b.String(); got != "first\nsecond" {
			t.Errorf("String() = %q", got)
		}
	})
	t.Run("dropped output loses the first, possibly cut, line", func(t *testing.T) {
		b := newTailBuffer(20)
		_, _ = b.Write([]byte(strings.Repeat("x", 30) + "\nkept line\n"))
		if got := b.String(); got != "kept line\n" {
			t.Errorf("String() = %q", got)
		}
	})
	t.Run("one long line cut inside a token returns nothing", func(t *testing.T) {
		b := newTailBuffer(20)
		// The kept bytes start after the token's prefix: a bare token body that no redactor
		// shape matches.
		_, _ = b.Write([]byte("ghs_" + strings.Repeat("A1", 40)))
		if got := b.String(); got != "" {
			t.Errorf("String() = %q, want empty", got)
		}
	})
}
