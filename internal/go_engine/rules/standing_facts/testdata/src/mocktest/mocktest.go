// Package mocktest is no standard library testing, though the faking fixture imports it under that name.
package mocktest

// T stands in for a test.
type T struct{}

// Log logs.
func (*T) Log(message string) {}
