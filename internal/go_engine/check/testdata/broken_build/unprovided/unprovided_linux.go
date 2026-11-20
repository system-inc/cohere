//go:build linux

// Package unprovided is linux's alone, its only file one linux compiles, and imports a path no module provides, so on
// linux go list reports it with a list error, the kind a tree missing its modules loads with, never a type error.
package unprovided

import "example.com/nowhere/absent"

// Width is read from the package no module provides.
const Width = absent.Width
