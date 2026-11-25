// Package result is a package whose name is a generic word, so its exported Result reads as result.Result where it
// is used, which is the package-name stutter and not this rule's.
package result

// Result is what the package hands out.
type Result struct{ Result int }

// Make is a Result holding count.
func Make(count int) Result { return Result{Result: count} }
