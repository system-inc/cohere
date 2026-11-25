// Package parity holds the assembly the parity test runs the original and the port over. The pass reads no Go, so
// the shapes that differ in Go (generics, aliases, method values, closures, embedded fields, dot imports) have no
// counterpart here; what it reads is the lines of each assembly file, and every shape those lines can take is in the
// files beside this one, one function for each, named for what it holds.
package parity
