// A second file of the package, after utils.go in go list's order, carries no finding: the package is reported once.
package utils

// Format is something else the package holds.
func Format(text string) string {
	return text
}
