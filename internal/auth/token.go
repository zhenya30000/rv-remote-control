package auth

import (
	"crypto/subtle"
	"strings"
)

func BearerMatches(header string, expected string) bool {
	const prefix = "Bearer "

	if expected == "" || !strings.HasPrefix(header, prefix) {
		return false
	}

	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))

	if len(provided) != len(expected) {
		return false
	}

	return subtle.ConstantTimeCompare(
		[]byte(provided),
		[]byte(expected),
	) == 1
}
