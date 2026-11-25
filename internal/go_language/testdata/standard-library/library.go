// Package library calls into a few standard-library packages, and holds two findings that read them: printf's, of a
// verb that does not fit, and structtag's, of a json tag that repeats one of log/slog's, whose position it names.
package library

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"
)

// Place is where something is, under the json name slog.Source gives its file.
type Place struct {
	Path string `json:"file"`
}

// Located is a source position beside a place, whose json names collide.
type Located struct {
	slog.Source
	Place
}

// Shout is text in capitals, formatted with a verb that does not fit it, or an error for bytes that are not text.
func Shout(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", errors.New("not text")
	}
	return fmt.Sprintf("%d", strings.ToUpper(text)), nil
}
