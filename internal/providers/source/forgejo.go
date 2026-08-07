package source

import "net/http"

// NewForgejoSource returns a Forgejo source provider for a self-hosted instance.
func NewForgejoSource(baseURL, token string, client *http.Client) (SourceProvider, error) {
	return NewGiteaCompatibleSource(baseURL, token, client)
}
