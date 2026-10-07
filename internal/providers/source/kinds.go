package source

// Supported source_kind values from specs §4 (monitored_repos.source_kind).
const (
	KindGitHub   = "github"
	KindGitLab   = "gitlab"
	KindGitea    = "gitea"
	KindForgejo  = "forgejo"
	KindCodeberg = "codeberg"
)

// ValidKinds lists all supported source_kind values.
var ValidKinds = []string{
	KindGitHub,
	KindGitLab,
	KindGitea,
	KindForgejo,
	KindCodeberg,
}

// IsValidKind reports whether kind is a supported source_kind value.
func IsValidKind(kind string) bool {
	switch kind {
	case KindGitHub, KindGitLab, KindGitea, KindForgejo, KindCodeberg:
		return true
	default:
		return false
	}
}
