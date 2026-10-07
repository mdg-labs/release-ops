package source

import "net/http"

// NewGiteaSource returns a Gitea source provider for a self-hosted instance.
func NewGiteaSource(baseURL, token string, client *http.Client) (SourceProvider, error) {
	return NewGiteaCompatibleSource(baseURL, token, client)
}
