package auth

import (
	"crypto/sha256"
	"sync"
	"time"
)

// spentStates holds the SHA-256 of every OAuth state redeemed by this process until the
// attempt would have expired anyway. The pre-login session a request reads is its own copy,
// saved when the request ends, so two requests carrying the same session can both match its
// state; the server is one process on one database, so this lock is the single writer that
// makes "one concurrent redemption wins" hold. The zero value is ready to use.
type spentStates struct {
	mu    sync.Mutex
	until map[[sha256.Size]byte]time.Time
}

// claim records state as spent until expires and reports whether this call was the first to.
func (s *spentStates) claim(state string, now, expires time.Time) bool {
	sum := sha256.Sum256([]byte(state))
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, t := range s.until {
		if !now.Before(t) {
			delete(s.until, k)
		}
	}
	if _, spent := s.until[sum]; spent {
		return false
	}
	if s.until == nil {
		s.until = make(map[[sha256.Size]byte]time.Time)
	}
	s.until[sum] = expires
	return true
}
