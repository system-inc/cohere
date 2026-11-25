// Package lookalike holds comments spelled like the directives that neither tool reads as one.
package lookalike

//nolintlint:allow-unused names another word that starts the same way
var longer = 1

//nolinted is no directive, since a letter follows the word
var lettered = 2

//lint no colon follows, so staticcheck reads no directive here
var colonless = 3

//linter:ignore is another tool's name
var otherTool = 4

//Lint:ignore U1000 is not staticcheck's spelling, which is lowercase
var capitalized = 5

//NOLINT is not golangci-lint's spelling, which is lowercase
var shouted = 6

//go:generate echo //nolint and //lint:ignore are this directive's arguments
var generated = 7
