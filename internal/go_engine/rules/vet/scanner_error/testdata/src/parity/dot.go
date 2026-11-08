package parity

import (
	. "bufio"
	"io"
	"strings"
)

// Found: bufio dot-imported. strings is not dot-imported beside it, since both declare NewReader and Reader.
func dotImported(reader io.Reader) {
	scanner := NewScanner(reader)
	for scanner.Scan() {
	}
}

// Clean: a reader in memory, beside a dot import.
func dotImportedInMemory(text string) {
	scanner := NewScanner(strings.NewReader(text))
	for scanner.Scan() {
	}
}
