package tickettemplate

import (
	"fmt"
	"strings"
	"text/template"
	"time"
)

// templateFuncs returns MVP helper functions (specs §5.4).
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatRFC3339": formatRFC3339,
		"formatDate":    formatDate,
		"yesNo":         yesNo,
	}
}

func formatRFC3339(value any) (string, error) {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return "", nil
		}
		return v.UTC().Format(time.RFC3339), nil
	case string:
		if strings.TrimSpace(v) == "" {
			return "", nil
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "", fmt.Errorf("formatRFC3339: %w", err)
		}
		return parsed.UTC().Format(time.RFC3339), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func formatDate(value any) (string, error) {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return "", nil
		}
		return v.UTC().Format("2006-01-02"), nil
	case string:
		if strings.TrimSpace(v) == "" {
			return "", nil
		}
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return "", fmt.Errorf("formatDate: %w", err)
		}
		return parsed.UTC().Format("2006-01-02"), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
