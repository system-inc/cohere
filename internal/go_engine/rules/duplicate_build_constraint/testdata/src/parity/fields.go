//go:build go1.1

/*
+build  x   y
+build y x
+buildx
+build
+buildx
+build
+build z
+build w
*/

// Fields split on any run of spaces, and only "+build " with its space opens a constraint.
package parity
