package main

// declarationKey names a declaration across builds: its package's path and its name, `T.m` for a method of the
// package-level named type `T`. A test variant's recompile of a package shares the package's path, so a declaration
// is counted once whichever variant holds it.
type declarationKey struct {
	packagePath string
	name        string
}

// absentAnswer is a declaration's answer in a build that does not declare it.
const absentAnswer = "(absent)"
