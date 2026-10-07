// Package integrationtester probes stored integration credentials via lightweight provider API calls.
package integrationtester

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

const (
	githubAPIBase    = "https://api.github.com"
	codebergBaseURL  = "https://codeberg.org"
	linearGraphQLURL = "https://api.linear.app/graphql"
	userAgent        = "release-ops"
)

// Tester validates integration credentials with provider-specific connectivity probes (specs §6.3–§6.4).
type Tester struct {
	client *http.Client
}

// New returns a connectivity tester. client may be nil to use a 30s default timeout client.
func New(client *http.Client) *Tester {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Tester{client: client}
}

// TestConnection probes the remote API for the given integration kind and decrypted secret JSON.
func (t *Tester) TestConnection(ctx context.Context, kind string, baseURL *string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch kind {
	case "github":
		return t.testGitHub(ctx, secret)
	case "gitlab":
		return t.testGitLab(ctx, baseURL, secret)
	case "gitea", "forgejo":
		return t.testGiteaCompatible(ctx, baseURL, secret)
	case "codeberg":
		return t.testCodeberg(ctx, secret)
	case "kaneo":
		return t.testKaneo(ctx, baseURL, secret)
	case "jira":
		return t.testJira(ctx, baseURL, secret)
	case "linear":
		return t.testLinear(ctx, secret)
	default:
		return fmt.Errorf("unsupported integration kind %q", kind)
	}
}

func (t *Tester) testGitHub(ctx context.Context, secret []byte) error {
	token, err := parseTokenPayload(secret)
	if err != nil {
		return err
	}

	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	if token == "" {
		// /user answers 401 without a token; /rate_limit is public and sends no credential.
		return t.doGET(ctx, githubAPIBase+"/rate_limit", headers)
	}
	headers["Authorization"] = "Bearer " + token
	return t.doGET(ctx, githubAPIBase+"/user", headers)
}

func (t *Tester) testGitLab(ctx context.Context, baseURL *string, secret []byte) error {
	root, err := requireBaseURL(baseURL, "gitlab")
	if err != nil {
		return err
	}
	token, err := parseTokenPayload(secret)
	if err != nil {
		return err
	}

	headers := map[string]string{}
	if token != "" {
		headers["PRIVATE-TOKEN"] = token
	}
	return t.doGET(ctx, root+"/api/v4/user", headers)
}

func (t *Tester) testGiteaCompatible(ctx context.Context, baseURL *string, secret []byte) error {
	root, err := requireBaseURL(baseURL, "gitea")
	if err != nil {
		return err
	}
	token, err := parseTokenPayload(secret)
	if err != nil {
		return err
	}

	return t.doGiteaCompatibleGET(ctx, root, token)
}

func (t *Tester) testCodeberg(ctx context.Context, secret []byte) error {
	token, err := parseTokenPayload(secret)
	if err != nil {
		return err
	}

	return t.doGiteaCompatibleGET(ctx, codebergBaseURL, token)
}

// doGiteaCompatibleGET probes the authenticated /api/v1/user with a token and the public
// /api/v1/version without one (/user answers 401 to an anonymous caller).
func (t *Tester) doGiteaCompatibleGET(ctx context.Context, root, token string) error {
	if token == "" {
		return t.doGET(ctx, root+"/api/v1/version", map[string]string{})
	}
	return t.doGET(ctx, root+"/api/v1/user", map[string]string{"Authorization": "token " + token})
}

func (t *Tester) testKaneo(ctx context.Context, baseURL *string, secret []byte) error {
	root, err := requireBaseURL(baseURL, "kaneo")
	if err != nil {
		return err
	}
	apiKey, err := parseKaneoPayload(secret)
	if err != nil {
		return err
	}

	apiBase := normalizeKaneoAPIBase(root)
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	return t.doGET(ctx, apiBase+"/auth/organization/list", headers)
}

func (t *Tester) testJira(ctx context.Context, baseURL *string, secret []byte) error {
	root, err := requireBaseURL(baseURL, "jira")
	if err != nil {
		return err
	}
	email, apiToken, err := parseJiraPayload(secret)
	if err != nil {
		return err
	}

	credentials := email + ":" + apiToken
	headers := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials)),
	}
	return t.doGET(ctx, root+"/rest/api/3/myself", headers)
}

func (t *Tester) testLinear(ctx context.Context, secret []byte) error {
	apiKey, err := parseLinearPayload(secret)
	if err != nil {
		return err
	}

	const query = `query { viewer { id } }`
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return fmt.Errorf("linear: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linearGraphQLURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("linear: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("linear: read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("linear: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var gqlResp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("linear: decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("linear: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

func (t *Tester) doGET(ctx context.Context, endpoint string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func parseTokenPayload(secret []byte) (string, error) {
	return source.ParseTokenSecret(secret)
}

func parseKaneoPayload(secret []byte) (string, error) {
	return ticket.ParseKaneoSecret(secret)
}

func parseJiraPayload(secret []byte) (email, apiToken string, err error) {
	return ticket.ParseJiraSecret(secret)
}

func parseLinearPayload(secret []byte) (string, error) {
	return ticket.ParseLinearSecret(secret)
}

func requireBaseURL(baseURL *string, kind string) (string, error) {
	if baseURL == nil || strings.TrimSpace(*baseURL) == "" {
		return "", fmt.Errorf("%s: base_url is required", kind)
	}
	normalized, err := normalizeBaseURL(*baseURL)
	if err != nil {
		return "", fmt.Errorf("%s base_url: %w", kind, err)
	}
	return normalized, nil
}

// normalizeKaneoAPIBase appends /api when missing (same semantics as ticket metadata provider).
func normalizeKaneoAPIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return raw
	}
	if strings.HasSuffix(raw, "/api") {
		return raw
	}
	return raw + "/api"
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("must not be empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("must use http or https scheme")
	}
	if parsed.Host == "" {
		return "", errors.New("must include a host")
	}

	return strings.TrimRight(raw, "/"), nil
}
