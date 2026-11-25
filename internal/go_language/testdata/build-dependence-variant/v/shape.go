// Package v has in-package tests on windows alone, so the host checks v and windows checks v [v.test] in its place,
// which holds shape_windows.go, a file of v only windows compiles.
package v

import "fmt"

// Shape is one type in every build, whose method set windows grows by String (shape_windows.go).
type Shape struct{ Side int }

// Printed formats a Shape, a fmt.Stringer on windows alone.
func Printed() string { return fmt.Sprint(Shape{Side: 2}) }

// Doubled reads nothing of Shape.
func Doubled(side int) int { return side * 2 }
