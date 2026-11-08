package parity

// This file imports one package, not in a group, so the first fix adds a declaration of its own before it.

import "os"

func singleImport(number int) {
	_ = os.Args
	_ = string(number)
	_ = Text(number)
}
