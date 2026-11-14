// Package githubtest is a fake GitHub API for tests: it checks the App JWT of every request
// against a generated key and answers the installation and access-token calls from recorded
// fixtures. It never reaches the network.
package githubtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:embed testdata/installation.json
var installationFixture []byte

//go:embed testdata/installation_token.json
var tokenFixture []byte

// TokenPrefix starts every token the fake mints; a counter follows.
const TokenPrefix = "fixture-installation-token-"

// AppID is the id of the App the fake serves.
const AppID int64 = 1234567

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	Body   string
	// JWTValid reports that the bearer was an App JWT signed by the fake's key, issued for
	// AppID, not expired and valid for at most ten minutes.
	JWTValid bool
}

// Fake is the fake GitHub API.
type Fake struct {
	// URL is the API base URL.
	URL string
	// PEM is the App's private key, PKCS#1.
	PEM []byte

	key *rsa.PrivateKey
	srv *httptest.Server

	mu sync.Mutex
	// now is the fake's clock for JWT expiry and token lifetimes.
	now          func() time.Time
	installs     map[int64]int64 // installation id -> app id answered
	tokenStatus  int
	tokenFor422  map[string]bool // repository names the installation does not cover
	requests     []Request
	mints        int
	tokenTTL     time.Duration
	installError int
}

// New starts a fake. now is the clock it measures JWT and token lifetimes with.
func New(t testing.TB, now func() time.Time) *Fake {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	f := &Fake{
		key:         key,
		now:         now,
		installs:    map[int64]int64{},
		tokenFor422: map[string]bool{},
		tokenTTL:    time.Hour,
		PEM:         pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.URL = f.srv.URL
	return f
}

// AddInstallation makes GET /app/installations/{id} answer the fixture for that id. The
// installation belongs to appID, which differs from AppID for another App's installation.
func (f *Fake) AddInstallation(id, appID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs[id] = appID
}

// FailInstallations makes every installation lookup answer status.
func (f *Fake) FailInstallations(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installError = status
}

// FailTokens makes every token request answer status; 0 restores normal answers.
func (f *Fake) FailTokens(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenStatus = status
}

// RejectRepository makes a token request that names the repository answer 422.
func (f *Fake) RejectRepository(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenFor422[name] = true
}

// Requests returns a copy of the requests received so far.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Mints returns how many tokens the fake has issued.
func (f *Fake) Mints() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mints
}

// TokenRequests returns the repository names of every token request that carried a body,
// in order; a request for every repository contributes nil.
func (f *Fake) TokenRequests() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, r := range f.requests {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.Path, "/access_tokens") {
			continue
		}
		var body struct {
			Repositories []string `json:"repositories"`
		}
		_ = json.Unmarshal([]byte(r.Body), &body)
		out = append(out, body.Repositories)
	}
	return out
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	valid := f.checkJWT(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))

	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Body: string(raw), JWTValid: valid})
	f.mu.Unlock()

	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/app/installations/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	idText, suffix, _ := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	appID, known := f.installs[id]
	switch {
	case r.Method == http.MethodGet && suffix == "":
		if f.installError != 0 {
			w.WriteHeader(f.installError)
			return
		}
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var doc map[string]any
		_ = json.Unmarshal(installationFixture, &doc)
		doc["id"] = id
		doc["app_id"] = appID
		_ = json.NewEncoder(w).Encode(doc)
	case r.Method == http.MethodPost && suffix == "access_tokens":
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			_, _ = w.Write([]byte(`{"message":"refused"}`))
			return
		}
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Repositories []string `json:"repositories"`
		}
		_ = json.Unmarshal(raw, &body)
		for _, name := range body.Repositories {
			if f.tokenFor422[name] {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
		}
		f.mints++
		var doc map[string]any
		_ = json.Unmarshal(tokenFixture, &doc)
		doc["token"] = fmt.Sprintf("%s%d", TokenPrefix, f.mints)
		doc["expires_at"] = f.now().Add(f.tokenTTL).UTC().Format(time.RFC3339)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(doc)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) checkJWT(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	enc := base64.RawURLEncoding
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		return false
	}
	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Iss int64 `json:"iss"`
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}
	now := f.now().Unix()
	return claims.Iss == AppID && claims.Exp > now && claims.Exp-claims.Iat <= 600 && claims.Iat <= now
}

// Clock is a manually advanced clock safe for concurrent use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock set to now.
func NewClock(now time.Time) *Clock { return &Clock{now: now} }

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
