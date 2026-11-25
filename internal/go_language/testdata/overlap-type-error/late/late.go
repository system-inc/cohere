// Package late imports early, so the load checks it after early, and it does not type-check.
package late

import "example.com/overlaptypeerror/early"

// Count is a number, given text.
var Count int = "text"

// Run describes the count.
func Run() { early.Describe(Count) }
