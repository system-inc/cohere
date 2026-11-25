// Package golden_tested holds a call in a file the package and its test variant share, and one in its test file, each
// with its golden file, so the shared file's golden file is held to each package's fixes once.
package golden_tested

func shared() {}

func kept() {}

func calls() {
	shared()
}
