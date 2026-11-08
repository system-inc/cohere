//go:build (go1.1 || x) && (x || go1.1) && (go1.1 || x)
// +build go1.1 x
// +build x go1.1
// +build go1.1 x

// Three identical lines are three pairs, so three findings.
package parity
