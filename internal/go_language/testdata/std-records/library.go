// Package library calls into a few standard-library packages, whose entries the system cache holds.
package library

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Shout is text in capitals, or an error for bytes that are not text.
func Shout(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", errors.New("not text")
	}
	return fmt.Sprintf("%s%s", strings.ToUpper(text), mode), nil
}
