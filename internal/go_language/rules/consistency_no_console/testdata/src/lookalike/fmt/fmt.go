// Package fmt is a package of another path that happens to share the standard library's name.
package fmt

// Println collects its values rather than writing them anywhere.
func Println(values ...any) []any {
	return values
}
