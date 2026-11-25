// Package u has in-package tests in every build, so the host checks u and u [u.test], and windows checks u [u.test]
// alone, which holds kind_windows.go, a file of u only windows compiles.
package u

import "fmt"

// Kind is one type in every build, whose method set windows grows by String (kind_windows.go).
type Kind struct{ Name string }

// Printed formats a Kind, a fmt.Stringer on windows alone.
func Printed() string { return fmt.Sprint(Kind{Name: "kind"}) }

// Doubled reads nothing of Kind.
func Doubled(count int) int { return count * 2 }
