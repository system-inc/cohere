package flagged

import "testing"

// A bare block in a test's subtest literal is judged as any other, a test file included.
func TestPlain(test *testing.T) {
	test.Parallel()
	test.Run("plain", func(test *testing.T) {
		test.Parallel()
		{ // want "These braces do nothing"
			if plain(nil) != 0 {
				test.Fatal("expecting zero")
			}
		}
	})
}
