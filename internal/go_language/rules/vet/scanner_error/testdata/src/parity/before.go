package parity

// Found or clean: the judgment compares this loop's position with laterScanner's declaration in parity.go, and which
// of the two files has the later positions follows the order the loader parses them in.
func scanLaterScannerFromAnotherFile() {
	for laterScanner.Scan() {
	}
}
