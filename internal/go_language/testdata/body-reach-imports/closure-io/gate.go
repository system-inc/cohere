// Package gate reads into a caller's buffer, which correctness-no-caller-data-mutation exempts only where io is in the
// package's import closure: dep imports io on the host alone, so the host exempts Read and windows reports it.
package gate

import _ "example.test/gate/dep"

// Source fills a buffer.
type Source struct{}

// Read writes into the caller's buffer, as io.Reader's contract allows.
func (source *Source) Read(buffer []byte) (int, error) {
	buffer[0] = 1
	return 1, nil
}
