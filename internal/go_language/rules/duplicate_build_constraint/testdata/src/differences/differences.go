//go:build (go1.1 || x) && (x || go1.1)
// +build go1.1 x
// +build x go1.1

// Package differences holds the shapes where the port's range or message could part from the original's: a
// comment inside the package clause, which the short range of the file spans to the package's name, and terms that
// print with quotes. The two agree on every one, so no difference is recorded.
package /* inside the clause */ differences
