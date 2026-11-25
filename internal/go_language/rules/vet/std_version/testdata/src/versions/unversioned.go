package versions

import (
	"strings"
	"sync"
)

// Summary calls into both versioned files. This file has no build line and the package no module, so the type
// checker records no version for it, and its use of strings.Lines, from go1.24, is not judged.
func Summary(text string) string {
	discarding().Info(trimmed(text))
	for line := range strings.Lines(text) {
		text += line
	}
	return described(&sync.Map{}) + strings.Repeat(".", counted(text, nil))
}
