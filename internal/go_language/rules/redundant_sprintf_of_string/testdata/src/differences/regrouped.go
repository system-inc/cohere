package differences

import "fmt"

// The file still uses fmt once every fix of it is applied, so the fixed file is judged by the fixes alone.
var _ = fmt.Sprint

func regrouped(first string, second string) string {
	// The original writes `first + second[1:]`, which compiles and slices second alone, so it means another string.
	return fmt.Sprintf("%s", first+second)[1:] // port moves: the argument is already a string, there's no need to use fmt.Sprintf
}
