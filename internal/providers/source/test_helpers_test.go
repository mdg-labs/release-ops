package source_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
)

type hostRewritingTransport struct {
	base *url.URL
	next http.RoundTripper
}

func (t hostRewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.base.Scheme
	cloned.URL.Host = t.base.Host
	cloned.Host = t.base.Host
	return t.next.RoundTrip(cloned)
}

func newHostRewritingClient(server *httptest.Server, host string) *http.Client {
	base, err := url.Parse(server.URL)
	if err != nil {
		panic(err)
	}
	transport := hostRewritingTransport{
		base: base,
		next: http.DefaultTransport,
	}
	return &http.Client{Transport: transport}
}
