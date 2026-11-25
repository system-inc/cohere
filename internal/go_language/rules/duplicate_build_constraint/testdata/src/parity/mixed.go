//go:build (go1.1 || x) && (go1.1 || y) && (x || go1.1)
// +build go1.1 x
// +build go1.1 y
// +build x go1.1

package parity

/*
+build after
+build after
*/
