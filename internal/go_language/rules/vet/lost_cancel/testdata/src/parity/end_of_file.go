package parity

import "context"

// The return cfg makes at this function's closing brace ends six bytes on, past the end of this file, so both report
// it as the point it starts at (golang/go#64547). It finds that the cancel function is not used on all paths, and
// this return, at the closing brace, which must be the file's last character before its final newline.
func lostAtTheEndOfTheFile() {
	_, cancel := context.WithCancel(context.Background())
	if condition {
		cancel()
	}
}
