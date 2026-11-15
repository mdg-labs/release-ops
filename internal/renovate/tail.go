package renovate

import (
	"bytes"
	"sync"
	"unicode/utf8"
)

// tailBuffer keeps the last bytes written to it, so a run that logs without end holds a bounded
// amount of memory.
type tailBuffer struct {
	mu      sync.Mutex
	max     int
	buf     []byte
	dropped bool
}

func newTailBuffer(max int) *tailBuffer {
	return &tailBuffer{max: max}
}

// Write implements io.Writer; it never fails and never blocks the process writing to it.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
		t.dropped = true
	}
	return len(p), nil
}

// String returns the kept tail. When output was dropped the first, possibly cut, line is left
// out, so no secret is half there for a redactor to miss; with no newline in what was kept, all
// of it is one cut line and nothing is returned.
func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buf
	if len(b) > t.max {
		b = b[len(b)-t.max:]
		t.dropped = true
	}
	if t.dropped {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			return ""
		}
		b = b[i+1:]
	}
	return string(b)
}

// tailBytes returns the last at most n bytes of s, starting on a character boundary.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}
