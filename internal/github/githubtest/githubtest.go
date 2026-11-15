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
	"slices"
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

//go:embed testdata/repository.json
var repositoryFixture []byte

//go:embed testdata/content_entry.json
var contentEntryFixture []byte

//go:embed testdata/hook_delivery.json
var hookDeliveryFixture []byte

// TokenPrefix starts every token the fake mints; a counter follows.
const TokenPrefix = "fixture-installation-token-"

// AppID is the id of the App the fake serves.
const AppID int64 = 1234567

// Request is one request the fake received.
type Request struct {
	Method string
	Path   string
	Body   string
	// Query is the raw query string.
	Query string
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

	listStatus  int                 // status of GET /app/installations; 0 answers the list
	suspended   map[int64]bool      // installation ids listed with suspended_at set
	repos       map[int64][]Repo    // installation id -> repositories it grants
	repoStatus  map[int64]int       // installation id -> status for its repository listing
	tokenOwner  map[string]int64    // token -> installation id it was minted for
	files       map[string][]string // owner/name -> paths of files, a trailing "/" marks a directory
	fileStatus  map[string]int      // owner/name -> status for its contents calls
	pageSize    int                 // entries per page for listings; 0 means everything on one page
	foreignNext string              // host a Link header names as the next page instead of the fake

	bots          map[string]int64 // lower-cased bot login (<slug>[bot]) -> user id
	botUserStatus int              // status for the bot user lookup; 0 answers normally

	users          map[int64]string             // user id -> current login
	orgMembers     map[string]Membership        // "org/login" (lower-cased) -> membership
	teamMembers    map[string]Membership        // "org/team/login" (lower-cased) -> membership
	memberStatus   int                          // status for every user and membership call; 0 answers normally
	userRepos      map[string]map[int64][]int64 // user token -> installation id -> repository ids
	userRepoStatus int                          // status for the user's repository listing; 0 answers normally

	deliveries       []Delivery         // newest first, as GitHub lists them
	deliveryStatus   int                // status of GET /app/hook/deliveries; 0 answers the list
	redeliveryStatus map[int64]int      // delivery id -> status for its redelivery; 0 answers 202
	onRedelivery     func(Delivery) int // called for each accepted redelivery; returns the receiver's status
}

// Delivery is a webhook delivery of the App as GET /app/hook/deliveries lists it.
type Delivery struct {
	ID   int64
	GUID string
	// DeliveredAt is when the delivery was attempted.
	DeliveredAt time.Time
	// StatusCode is the status the receiver answered; 0 stands for no answer at all.
	StatusCode int
	Event      string
	Redelivery bool
}

// Membership is the state and role the fake reports for an organization or team membership.
type Membership struct{ State, Role string }

// Repo is a repository an installation grants.
type Repo struct {
	ID            int64
	FullName      string
	DefaultBranch string
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
		suspended:   map[int64]bool{},
		repos:       map[int64][]Repo{},
		repoStatus:  map[int64]int{},
		tokenOwner:  map[string]int64{},
		bots:        map[string]int64{},
		users:       map[int64]string{},
		orgMembers:  map[string]Membership{},
		teamMembers: map[string]Membership{},
		userRepos:   map[string]map[int64][]int64{},
		files:       map[string][]string{},
		fileStatus:  map[string]int{},

		redeliveryStatus: map[int64]int{},
		tokenTTL:         time.Hour,
		PEM:              pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.URL = f.srv.URL
	return f
}

// SetBotUser makes the public GET /users/{slug}[bot] answer the bot user of the App with that
// slug, with the numeric id given.
func (f *Fake) SetBotUser(slug string, id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bots[strings.ToLower(slug+"[bot]")] = id
}

// FailBotUser makes the bot user lookup answer status; zero answers normally again.
func (f *Fake) FailBotUser(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.botUserStatus = status
}

// AddInstallation makes GET /app/installations/{id} answer the fixture for that id. The
// installation belongs to appID, which differs from AppID for another App's installation.
func (f *Fake) AddInstallation(id, appID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs[id] = appID
}

// FailInstallationList makes GET /app/installations answer status; 0 restores normal answers.
func (f *Fake) FailInstallationList(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listStatus = status
}

// RemoveInstallation makes GET /app/installations stop listing the installation.
func (f *Fake) RemoveInstallation(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.installs, id)
}

// SuspendInstallation lists the installation with suspended_at set, or without it.
func (f *Fake) SuspendInstallation(id int64, suspended bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspended[id] = suspended
}

// SetRepositories sets the repositories GET /installation/repositories lists for a token of
// the installation.
func (f *Fake) SetRepositories(installationID int64, repos ...Repo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[installationID] = repos
}

// FailRepositories makes the repository listing of the installation answer status; 0
// restores normal answers.
func (f *Fake) FailRepositories(installationID int64, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repoStatus[installationID] = status
}

// SetFiles sets the files of a repository's default branch, as paths; a path ending in "/"
// is an empty directory.
func (f *Fake) SetFiles(fullName string, paths ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[fullName] = paths
}

// FailContents makes every contents call of the repository answer status; 0 restores
// normal answers.
func (f *Fake) FailContents(fullName string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileStatus[fullName] = status
}

// SetPageSize makes listings answer that many entries a page with a Link header.
func (f *Fake) SetPageSize(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pageSize = n
}

// LinkTo makes listings name host (scheme://host) as the next page's host instead of the fake.
func (f *Fake) LinkTo(host string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.foreignNext = host
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

// SetUser makes GET /user/{id} answer the login for the account.
func (f *Fake) SetUser(id int64, login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[id] = login
}

// SetOrganizationMember makes the organization membership of login answer state and role;
// without a call the user is not a member.
func (f *Fake) SetOrganizationMember(org, login, state, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orgMembers[strings.ToLower(org+"/"+login)] = Membership{State: state, Role: role}
}

// RemoveOrganizationMember removes the user from the organization again.
func (f *Fake) RemoveOrganizationMember(org, login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.orgMembers, strings.ToLower(org+"/"+login))
}

// SetTeamMember makes the team membership of login answer state and role; without a call
// the user is not in the team.
func (f *Fake) SetTeamMember(org, team, login, state, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.teamMembers[strings.ToLower(org+"/"+team+"/"+login)] = Membership{State: state, Role: role}
}

// FailMemberships makes every user and membership call answer status; 0 restores normal
// answers.
func (f *Fake) FailMemberships(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memberStatus = status
}

// SetUserRepositories makes GET /user/installations/{id}/repositories list the repository
// ids for the user token.
func (f *Fake) SetUserRepositories(userToken string, installationID int64, repositoryIDs ...int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userRepos[userToken] == nil {
		f.userRepos[userToken] = map[int64][]int64{}
	}
	f.userRepos[userToken][installationID] = repositoryIDs
}

// FailUserRepositories makes the user's repository listing answer status; 0 restores
// normal answers.
func (f *Fake) FailUserRepositories(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userRepoStatus = status
}

// SetDeliveries sets the deliveries GET /app/hook/deliveries lists, newest first.
func (f *Fake) SetDeliveries(deliveries ...Delivery) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveries = deliveries
}

// FailDeliveryList makes GET /app/hook/deliveries answer status; 0 restores normal answers.
func (f *Fake) FailDeliveryList(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveryStatus = status
}

// FailRedelivery makes the redelivery of the delivery answer status; 0 restores 202.
func (f *Fake) FailRedelivery(id int64, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redeliveryStatus[id] = status
}

// OnRedelivery sets what happens when GitHub is asked to redeliver: fn gets the delivery
// being repeated, outside the fake's lock, and returns the status the receiver answered. The
// fake then lists a new attempt of the same GUID with that status, newest first, as GitHub
// does. Without it a redelivery is accepted and nothing is listed.
func (f *Fake) OnRedelivery(fn func(Delivery) int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onRedelivery = fn
}

// Redeliveries returns the ids of the deliveries redelivery was requested for, in order.
func (f *Fake) Redeliveries() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []int64
	for _, r := range f.requests {
		rest, ok := strings.CutPrefix(r.Path, "/app/hook/deliveries/")
		idText, suffix, _ := strings.Cut(rest, "/")
		if !ok || r.Method != http.MethodPost || suffix != "attempts" {
			continue
		}
		if id, err := strconv.ParseInt(idText, 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
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
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(raw), JWTValid: valid})
	f.mu.Unlock()

	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if login, ok := strings.CutPrefix(r.URL.Path, "/users/"); ok {
		f.serveBotUser(w, login)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/user/installations/") {
		f.serveUserRepositories(w, r, bearer)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/installation/") || strings.HasPrefix(r.URL.Path, "/orgs/") || strings.HasPrefix(r.URL.Path, "/user/") || strings.HasPrefix(r.URL.Path, "/repos/") {
		f.serveWithToken(w, r, bearer)
		return
	}
	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/app/installations" {
		f.serveInstallationList(w, r)
		return
	}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/app/hook/deliveries"); ok {
		f.serveDeliveries(w, r, rest)
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
	_, known := f.installs[id]
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
		_ = json.NewEncoder(w).Encode(f.installationDoc(id))
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
		f.tokenOwner[doc["token"].(string)] = id
		doc["expires_at"] = f.now().Add(f.tokenTTL).UTC().Format(time.RFC3339)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(doc)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) deliveryDoc(d Delivery) map[string]any {
	var doc map[string]any
	_ = json.Unmarshal(hookDeliveryFixture, &doc)
	doc["id"] = d.ID
	doc["guid"] = d.GUID
	doc["delivered_at"] = d.DeliveredAt.UTC().Format(time.RFC3339)
	doc["status_code"] = d.StatusCode
	doc["redelivery"] = d.Redelivery
	if d.Event != "" {
		doc["event"] = d.Event
	}
	if d.StatusCode >= 200 && d.StatusCode < 300 {
		doc["status"] = "OK"
	} else {
		doc["status"] = fmt.Sprintf("Invalid HTTP Response: %d", d.StatusCode)
	}
	return doc
}

// serveDeliveries answers the list of deliveries and a redelivery request; rest is the path
// after /app/hook/deliveries.
func (f *Fake) serveDeliveries(w http.ResponseWriter, r *http.Request, rest string) {
	f.mu.Lock()
	if rest == "" && r.Method == http.MethodGet {
		defer f.mu.Unlock()
		if f.deliveryStatus != 0 {
			w.WriteHeader(f.deliveryStatus)
			return
		}
		entries := make([]any, len(f.deliveries))
		for i, d := range f.deliveries {
			entries[i] = f.deliveryDoc(d)
		}
		f.page(w, r, entries, func(e []any) any { return e })
		return
	}
	idText, ok := strings.CutSuffix(strings.TrimPrefix(rest, "/"), "/attempts")
	id, err := strconv.ParseInt(idText, 10, 64)
	if r.Method != http.MethodPost || !ok || err != nil {
		f.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	var original *Delivery
	for i := range f.deliveries {
		if f.deliveries[i].ID == id {
			original = &f.deliveries[i]
			break
		}
	}
	if original == nil {
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if status := f.redeliveryStatus[id]; status != 0 {
		f.mu.Unlock()
		w.WriteHeader(status)
		return
	}
	d, hook := *original, f.onRedelivery
	f.mu.Unlock()

	if hook != nil {
		status := hook(d)
		f.mu.Lock()
		next := slices.MaxFunc(f.deliveries, func(a, b Delivery) int { return int(a.ID - b.ID) }).ID + 1
		d.ID, d.StatusCode, d.Redelivery, d.DeliveredAt = next, status, true, f.now()
		f.deliveries = append([]Delivery{d}, f.deliveries...)
		f.mu.Unlock()
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{}`))
}

func (f *Fake) installationDoc(id int64) map[string]any {
	var doc map[string]any
	_ = json.Unmarshal(installationFixture, &doc)
	doc["id"] = id
	doc["app_id"] = f.installs[id]
	if f.suspended[id] {
		doc["suspended_at"] = "2026-11-14T11:00:00Z"
	}
	return doc
}

// page writes entries as one page of a listing: all of them, or pageSize of them with a
// Link header to the next page.
func (f *Fake) page(w http.ResponseWriter, r *http.Request, entries []any, wrap func([]any) any) {
	size := f.pageSize
	if size <= 0 {
		size = len(entries)
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if n < 1 {
		n = 1
	}
	from := min((n-1)*size, len(entries))
	to := min(from+size, len(entries))
	if to < len(entries) {
		host := f.srv.URL
		if f.foreignNext != "" {
			host = f.foreignNext
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?per_page=100&page=%d>; rel="next"`, host, r.URL.Path, n+1))
	}
	_ = json.NewEncoder(w).Encode(wrap(entries[from:to]))
}

func (f *Fake) serveInstallationList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listStatus != 0 {
		w.WriteHeader(f.listStatus)
		return
	}
	ids := make([]int64, 0, len(f.installs))
	for id := range f.installs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	entries := make([]any, len(ids))
	for i, id := range ids {
		entries[i] = f.installationDoc(id)
	}
	f.page(w, r, entries, func(e []any) any { return e })
}

func (f *Fake) serveWithToken(w http.ResponseWriter, r *http.Request, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	installationID, ok := f.tokenOwner[token]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/orgs/") || strings.HasPrefix(r.URL.Path, "/user/") {
		f.serveDirectory(w, r)
		return
	}

	if r.URL.Path == "/installation/repositories" {
		if status := f.repoStatus[installationID]; status != 0 {
			w.WriteHeader(status)
			return
		}
		entries := make([]any, len(f.repos[installationID]))
		for i, repo := range f.repos[installationID] {
			var doc map[string]any
			_ = json.Unmarshal(repositoryFixture, &doc)
			doc["id"] = repo.ID
			doc["full_name"] = repo.FullName
			doc["name"] = repo.FullName[strings.Index(repo.FullName, "/")+1:]
			doc["default_branch"] = repo.DefaultBranch
			entries[i] = doc
		}
		f.page(w, r, entries, func(e []any) any {
			return map[string]any{"total_count": len(entries), "repositories": e}
		})
		return
	}

	rest, ok := strings.CutPrefix(r.URL.Path, "/repos/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 || parts[2] != "contents" {
		http.NotFound(w, r)
		return
	}
	fullName := parts[0] + "/" + parts[1]
	if status := f.fileStatus[fullName]; status != 0 {
		w.WriteHeader(status)
		return
	}
	paths, known := f.files[fullName]
	if !known {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	dir := ""
	if len(parts) == 4 {
		dir = parts[3] + "/"
	}
	seen := map[string]bool{}
	var entries []any
	for _, p := range paths {
		rel, under := strings.CutPrefix(p, dir)
		if !under || rel == "" {
			continue
		}
		name, _, nested := strings.Cut(rel, "/")
		kind := "file"
		if nested {
			kind = "dir"
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		var doc map[string]any
		_ = json.Unmarshal(contentEntryFixture, &doc)
		doc["name"] = name
		doc["path"] = dir + name
		doc["type"] = kind
		entries = append(entries, doc)
	}
	if entries == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(entries)
}

// serveBotUser answers the public GET /users/{login} for a bot user the fake knows.
func (f *Fake) serveBotUser(w http.ResponseWriter, login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.botUserStatus != 0 {
		w.WriteHeader(f.botUserStatus)
		return
	}
	id, ok := f.bots[strings.ToLower(login)]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "login": login, "type": "Bot"})
}

// serveDirectory answers the user and membership calls an installation token makes; the
// caller holds the lock.
func (f *Fake) serveDirectory(w http.ResponseWriter, r *http.Request) {
	if f.memberStatus != 0 {
		w.WriteHeader(f.memberStatus)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "user":
		id, err := strconv.ParseInt(parts[1], 10, 64)
		login, ok := f.users[id]
		if err != nil || !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "login": login, "type": "User"})
	case len(parts) == 4 && parts[0] == "orgs" && parts[2] == "memberships":
		f.writeMembership(w, f.orgMembers, strings.ToLower(parts[1]+"/"+parts[3]))
	case len(parts) == 6 && parts[0] == "orgs" && parts[2] == "teams" && parts[4] == "memberships":
		f.writeMembership(w, f.teamMembers, strings.ToLower(parts[1]+"/"+parts[3]+"/"+parts[5]))
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) writeMembership(w http.ResponseWriter, members map[string]Membership, key string) {
	m, ok := members[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"state": m.State, "role": m.Role})
}

// serveUserRepositories answers GET /user/installations/{id}/repositories for a user token.
func (f *Fake) serveUserRepositories(w http.ResponseWriter, r *http.Request, token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	byInstallation, known := f.userRepos[token]
	if !known {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.userRepoStatus != 0 {
		w.WriteHeader(f.userRepoStatus)
		return
	}
	idText, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/user/installations/"), "/repositories")
	installationID, err := strconv.ParseInt(idText, 10, 64)
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	ids := byInstallation[installationID]
	entries := make([]any, len(ids))
	for i, id := range ids {
		var doc map[string]any
		_ = json.Unmarshal(repositoryFixture, &doc)
		doc["id"] = id
		entries[i] = doc
	}
	f.page(w, r, entries, func(e []any) any {
		return map[string]any{"total_count": len(entries), "repositories": e}
	})
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
