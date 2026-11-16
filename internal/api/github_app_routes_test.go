package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const rotatePath = "/api/v1/github-app/private-key"

// appKeyServer is the production router over a fake GitHub API, with the App stored under the
// key the fake accepts.
type appKeyServer struct {
	srv  *httptest.Server
	deps *ServerDeps
	fake *githubtest.Fake
	// app signs as the stored App through the store, as the server's own client does.
	app *github.AppClient
}

func newAppKeyServer(t *testing.T, user github.User) *appKeyServer {
	t.Helper()
	deps := newRoutesTestDeps(t)
	deps.AppURL = "https://release-ops.example.com"
	deps.GitHubOAuth = stubOAuth{user: user}
	storetest.SeedRepositories(t, deps.Store.DB(), 1)

	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1, githubtest.AppID)
	fake.SetUser(rolesOwnerID, "acme-owner")
	fake.SetUser(rolesMemberID, "acme-member")
	fake.SetOrganizationMember("acme", "acme-owner", "active", "admin")
	fake.SetOrganizationMember("acme", "acme-member", "active", "member")
	app := sampleSignInApp()
	app.AppID = githubtest.AppID
	app.PrivateKey = string(fake.PEM)
	if err := deps.Store.GitHubApp().Create(context.Background(), app); err != nil {
		t.Fatalf("store app: %v", err)
	}

	client := github.NewClient(fake.URL, 5*time.Second)
	deps.GitHub = client
	deps.Roles = &auth.GitHubRoles{
		Access: deps.Store.Access(),
		Users:  deps.Store.SignIn(),
		GitHub: github.NewAppClient(client, fixedCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{}),
	}

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	return &appKeyServer{
		srv: srv, deps: deps, fake: fake,
		app: github.NewAppClient(client, deps.Store.GitHubApp(), github.AppClientOptions{}),
	}
}

func (s *appKeyServer) signIn(t *testing.T) *http.Client {
	t.Helper()
	return (&rolesServer{srv: s.srv}).signIn(t)
}

func (s *appKeyServer) rotate(t *testing.T, c *http.Client, privateKey string) (int, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"privateKey": privateKey})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, s.srv.URL+rotatePath, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", rotatePath, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func throwawayKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// storedKey returns the stored private key as the store decrypts it, and the raw column.
func (s *appKeyServer) storedKey(t *testing.T) (plain []byte, column, keyID string) {
	t.Helper()
	_, plain, err := s.deps.Store.GitHubApp().SigningKey(context.Background())
	if err != nil {
		t.Fatalf("SigningKey: %v", err)
	}
	if err := s.deps.DB.QueryRow(`SELECT encrypted_private_key, key_id FROM github_app WHERE id = 1`).Scan(&column, &keyID); err != nil {
		t.Fatalf("read github_app: %v", err)
	}
	return plain, column, keyID
}

func TestAdminRotatesThePrivateKeyThroughTheRouter(t *testing.T) {
	t.Parallel()
	s := newAppKeyServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	oldPEM := string(s.fake.PEM)
	if _, err := s.deps.DB.Exec(`UPDATE github_app SET key_id = 'stale'`); err != nil {
		t.Fatalf("set stale key id: %v", err)
	}

	// A token minted under the old key is cached before the rotation.
	if _, err := s.app.Token(context.Background(), 1); err != nil {
		t.Fatalf("Token: %v", err)
	}
	mintsBefore := s.fake.Mints()

	newPEM := s.fake.AddKey(t)
	code, body := s.rotate(t, admin, string(newPEM))
	if code != http.StatusNoContent || body != "" {
		t.Fatalf("rotate = %d %q, want 204 with no body", code, body)
	}

	plain, column, keyID := s.storedKey(t)
	if string(plain) != string(newPEM) {
		t.Errorf("the stored key is not the uploaded one")
	}
	if strings.Contains(column, "PRIVATE KEY") || strings.Contains(column, string(newPEM)) || strings.Contains(column, oldPEM) {
		t.Errorf("the key is not encrypted at rest")
	}
	cipher, err := crypto.NewCipherFromHex(routesTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	if keyID != cipher.KeyID() {
		t.Errorf("key_id = %q, want %q: the id is written again with the new value", keyID, cipher.KeyID())
	}

	// The old key is deleted at GitHub; a token must now come from the new key, so the cached
	// one is not served.
	s.fake.RevokeKey(t, []byte(oldPEM))
	if _, err := s.app.Token(context.Background(), 1); err != nil {
		t.Fatalf("Token after the rotation: %v", err)
	}
	if got := s.fake.Mints(); got != mintsBefore+1 {
		t.Errorf("mints after the rotation = %d, want %d: the cached token of the old key was served", got, mintsBefore+1)
	}
}

func TestRejectedKeysLeaveTheStoredKeyUnchanged(t *testing.T) {
	t.Parallel()
	s := newAppKeyServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	before, columnBefore, keyIDBefore := s.storedKey(t)

	// The mismatch case comes last: it leaves the fake answering for another App.
	cases := []struct {
		name       string
		key        func() string
		prepare    func()
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not a PEM",
			key:        func() string { return "not a key" },
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_ERROR",
		},
		{
			name:       "empty",
			key:        func() string { return "" },
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_ERROR",
		},
		{
			name:       "another App's key",
			key:        func() string { return string(throwawayKey(t)) },
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_ERROR",
		},
		{
			name:       "GitHub answers for another App",
			key:        func() string { return string(s.fake.AddKey(t)) },
			prepare:    func() { s.fake.AnswerAppID(999) },
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_ERROR",
		},
	}
	var firstBody string
	for i, tc := range cases {
		name := tc.name
		if tc.prepare != nil {
			tc.prepare()
		}
		key := tc.key()
		code, body := s.rotate(t, admin, key)
		if i == 0 {
			firstBody = body
		} else if body != firstBody {
			t.Errorf("%s: response %s differs from %s: a rejection must not tell which check failed", name, body, firstBody)
		}
		if code != tc.wantStatus || !strings.Contains(body, `"code":"`+tc.wantCode+`"`) {
			t.Errorf("%s: rotate = %d %s, want %d %s", name, code, body, tc.wantStatus, tc.wantCode)
		}
		if key != "" && strings.Contains(body, strings.TrimSpace(key)) {
			t.Errorf("%s: the response echoes the key", name)
		}
		after, columnAfter, keyIDAfter := s.storedKey(t)
		if string(after) != string(before) || columnAfter != columnBefore || keyIDAfter != keyIDBefore {
			t.Errorf("%s: the stored key changed", name)
		}
	}
}

func TestAnUnreachableGitHubLeavesTheStoredKeyUnchanged(t *testing.T) {
	t.Parallel()
	s := newAppKeyServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	_, columnBefore, _ := s.storedKey(t)

	newPEM := s.fake.AddKey(t)
	s.fake.Close()
	code, body := s.rotate(t, admin, string(newPEM))
	if code != http.StatusBadGateway || !strings.Contains(body, `"code":"GITHUB_UNAVAILABLE"`) {
		t.Fatalf("rotate with GitHub down = %d %s, want 502 GITHUB_UNAVAILABLE", code, body)
	}
	if _, column, _ := s.storedKey(t); column != columnBefore {
		t.Error("the stored key changed although GitHub could not check it")
	}
}

func TestViewerGets403OnKeyRotationAndTheKeyIsUnchanged(t *testing.T) {
	t.Parallel()
	s := newAppKeyServer(t, github.User{ID: rolesMemberID, Login: "acme-member"})
	viewer := s.signIn(t)
	_, columnBefore, _ := s.storedKey(t)

	code, body := s.rotate(t, viewer, string(s.fake.AddKey(t)))
	if code != http.StatusForbidden || !strings.Contains(body, `"code":"FORBIDDEN"`) {
		t.Fatalf("viewer rotate = %d %s, want 403 FORBIDDEN", code, body)
	}
	if _, column, _ := s.storedKey(t); column != columnBefore {
		t.Error("a viewer replaced the stored key")
	}
}

func TestKeyRotationNeedsASession(t *testing.T) {
	t.Parallel()
	s := newAppKeyServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	code, _ := s.rotate(t, noRedirects(t), string(s.fake.AddKey(t)))
	if code != http.StatusUnauthorized {
		t.Fatalf("rotate without a session = %d, want 401", code)
	}
}
