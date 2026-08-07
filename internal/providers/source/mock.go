package source

import "context"

// MockSourceProvider is a test double for SourceProvider.
type MockSourceProvider struct {
	Release *Release
	Err     error
}

// GetLatestRelease returns the configured release or error after checking ctx.
func (m *MockSourceProvider) GetLatestRelease(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Release, nil
}
