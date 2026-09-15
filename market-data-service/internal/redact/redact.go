package redact

import "strings"

func Secret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "[redacted]"
}

func Address(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if strings.Contains(value, "@") || strings.Contains(strings.ToLower(value), "password") {
		return "[redacted]"
	}
	return value
}
