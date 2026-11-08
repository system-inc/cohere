// A //line directive renames the file in positions, and the file's own name still decides whether it is a test: this
// file is not one, though it says it is.
//line renamed_test.go:1
//go:debug panicnil=1
package parity
