// Package outside holds Value, which the test restores, with no answer stored, so a question of it must reload.
package outside

// Value is a pointer the caller checks.
func Value() *int {
	return new(int)
}
