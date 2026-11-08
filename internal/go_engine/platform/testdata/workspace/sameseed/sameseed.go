// Package sameseed compiles one more file under windows, unexported, so its file set differs and its export does not.
package sameseed

// Value is what its importer reads.
func Value() int { return 1 }
