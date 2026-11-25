// Package p holds a struct W that embeds T. Windows alone compiles an in-package test file, which declares a method
// on T, so in the test variant windows builds W's method set holds the promoted M, and in the plain package it holds
// nothing.
package p

// T is the embedded type, with no method in the package's own files.
type T struct{}

// W embeds T by value, so a value method of T is promoted into its method set.
type W struct{ T }
