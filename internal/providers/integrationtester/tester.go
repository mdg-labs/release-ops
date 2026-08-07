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
	case "phasical":
		return t.testPhasical(ctx, baseURL, secret)
	case "jira":
		return t.testJira(ctx, baseURL, secret)
	case "linear":
		return t.testLinear(ctx, secret)
	default:
		return fmt.Errorf("unsupported integration kind %q", kind)
	}
}

type tokenPayload struct {
	Token string `json:"token"`
}

type phasicalPayload struct {
	APIKey string `json:"api_key"`
}

type jiraPayload struct {
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

type linearPayload struct {
	APIKey string `json:"api_key"`
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
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
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

	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "token " + token
	}
	return t.doGET(ctx, root+"/api/v1/user", headers)
}

func (t *Tester) testCodeberg(ctx context.Context, secret []byte) error {
	token, err := parseTokenPayload(secret)
	if err != nil {
		return err
	}

	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "token " + token
	}
	return t.doGET(ctx, codebergBaseURL+"/api/v1/user", headers)
}

func (t *Tester) testPhasical(ctx context.Context, baseURL *string, secret []byte) error {
	root, err := requireBaseURL(baseURL, "phasical")
	if err != nil {
		return err
	}
	apiKey, err := parsePhasicalPayload(secret)
	if err != nil {
		return err
	}

	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	return t.doGET(ctx, root+"/me", headers)
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
	var creds tokenPayload
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	return strings.TrimSpace(creds.Token), nil
}

func parsePhasicalPayload(secret []byte) (string, error) {
	var creds phasicalPayload
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	return strings.TrimSpace(creds.APIKey), nil
}

func parseJiraPayload(secret []byte) (email, apiToken string, err error) {
	var creds jiraPayload
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", "", fmt.Errorf("parse integration secret: %w", err)
	}
	email = strings.TrimSpace(creds.Email)
	apiToken = strings.TrimSpace(creds.APIToken)
	if email == "" {
		return "", "", errors.New("jira: email is required in integration secret")
	}
	if apiToken == "" {
		return "", "", errors.New("jira: api_token is required in integration secret")
	}
	return email, apiToken, nil
}

func parseLinearPayload(secret []byte) (string, error) {
	var creds linearPayload
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	apiKey := strings.TrimSpace(creds.APIKey)
	if apiKey == "" {
		return "", errors.New("linear: api_key is required in integration secret")
	}
	return apiKey, nil
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
