package middleware

// Scope is the set of repositories a request may see (specs.html § 3.9). An admin's scope is
// every repository; a viewer's is the list stored with the session. The zero Scope allows
// nothing, so a request that never got a scope, or a session with no stored list, sees no
// repository.
type Scope struct {
	all bool
	ids map[string]struct{}
}

// AllRepositories is the admin scope.
func AllRepositories() Scope { return Scope{all: true} }

// RepositoryScope is the scope of the repositories with the given row ids.
func RepositoryScope(ids []string) Scope {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return Scope{ids: set}
}

// All reports whether the scope is every repository.
func (s Scope) All() bool { return s.all }

// Allows reports whether the repository with the row id is in scope. A handler that acts on
// or returns one repository answers 404 when it is not, so its existence is not revealed.
func (s Scope) Allows(repositoryID string) bool {
	if s.all {
		return true
	}
	_, ok := s.ids[repositoryID]
	return ok
}

// IDs returns the row ids of a limited scope for a query's filter; ok is false for the
// all-repositories scope, which has no list to filter by.
func (s Scope) IDs() (ids []string, ok bool) {
	if s.all {
		return nil, false
	}
	ids = make([]string, 0, len(s.ids))
	for id := range s.ids {
		ids = append(ids, id)
	}
	return ids, true
}

// FilterByRepository returns the items whose repository is in scope, in their order. Every
// list endpoint of repository-scoped data passes its rows through it, or filters its query
// by Scope.IDs.
func FilterByRepository[T any](s Scope, items []T, repositoryID func(T) string) []T {
	if s.all {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if s.Allows(repositoryID(item)) {
			out = append(out, item)
		}
	}
	return out
}
