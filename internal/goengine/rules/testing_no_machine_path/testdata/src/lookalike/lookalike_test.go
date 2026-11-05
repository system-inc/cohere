// Package lookalike holds text that resembles a home directory and is not one, so the rule stays quiet on all of it.
package lookalike

// ahra pinned /Users/someone/Projects/ahra, in a comment only, which is no literal.
const repositoryRoot = "/repo"

// A word that ends in Users.
const allUsers = "/AllUsers/x/y"

// A home directory alone, with no name after it.
const usersAlone = "/Users/"

const homeAlone = "/home/"

// A name with no slash after it is not a directory inside a home.
const noTrailingSlash = "/Users/someone"

// A Users segment a word runs into is part of another path.
const runInto = "relative/Users/someone/x"

// The expression is case-sensitive, as testpolicy's is.
const lowerCaseUsers = "/users/someone/x"

// A name with a space in it is not one path segment.
const spacedName = "/Users/some one/x"

// The identifier Users is no literal.
var Users = []string{
	repositoryRoot,
	allUsers,
	usersAlone,
	homeAlone,
	noTrailingSlash,
	runInto,
	lowerCaseUsers,
	spacedName,
}
