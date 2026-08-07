package ticket

import (
	"strings"
)

// ClassifyStatus maps a provider-native status slug/name/id to open, done, cancelled, or unknown
// using ticket_projects.status_mapping (specs §5.2, §6).
//
// Matching is case-insensitive. Priority: open, then done, then cancelled.
func ClassifyStatus(mapping StatusMapping, rawStatus string) string {
	rawStatus = strings.TrimSpace(rawStatus)
	if rawStatus == "" {
		return StatusUnknown
	}

	if matchesStatus(mapping.Open, rawStatus) {
		return StatusOpen
	}
	if matchesStatus(mapping.Done, rawStatus) {
		return StatusDone
	}
	if matchesStatus(mapping.Cancelled, rawStatus) {
		return StatusCancelled
	}

	return StatusUnknown
}

func matchesStatus(values []string, rawStatus string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), rawStatus) {
			return true
		}
	}
	return false
}
