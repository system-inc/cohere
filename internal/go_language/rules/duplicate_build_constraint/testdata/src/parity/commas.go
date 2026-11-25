//go:build ((go1.1 && gc) || x) && ((gc && go1.1) || x)
// +build go1.1,gc x
// +build gc,go1.1 x

// A term is compared as written, so go1.1,gc and gc,go1.1 differ.
package parity
