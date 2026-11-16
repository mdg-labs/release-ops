package tickettemplate

// Context is the root template data object (specs §5.4). Every value is a plain string, a
// number or a list of such values: content from GitHub and Renovate is data, never template
// source, so no value carries methods or is parsed.
type Context struct {
	PullRequest  PullRequestContext
	Repository   string
	Dependencies []DependencyContext
	ReleaseNotes string
	// Supersede is populated for the supersede comment only.
	Supersede SupersedeContext
}

// PullRequestContext is the update PR a ticket follows.
type PullRequestContext struct {
	URL    string
	Number int64
	Title  string
}

// DependencyContext is one dependency of the PR; From, To and UpdateType are empty when
// unknown.
type DependencyContext struct {
	Name       string
	From       string
	To         string
	UpdateType string
}

// SupersedeContext describes the PR that replaced the one the ticket follows (specs §5.2.1).
type SupersedeContext struct {
	NewPullRequest PullRequestContext
	NewTicketURL   string
}
