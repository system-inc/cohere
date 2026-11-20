// Package whole type-checks in every build, so the object differential compares it whatever the rest of the
// broken_build fixture does (#3096be2).
package whole

// Answer is the same in every build.
const Answer = 42
