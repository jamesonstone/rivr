package manifest

import (
	"crypto/rand"
	"fmt"
	"strings"
	"unicode"
)

func Slug(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func NewProjectID(slug string) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate project id: %w", err)
	}
	var suffix strings.Builder
	for _, value := range bytes {
		suffix.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return Slug(slug) + "-" + suffix.String(), nil
}
