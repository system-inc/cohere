// Package m declares Finish, which ends the process on windows alone, through a function each platform's file
// declares, so its no-return fact differs on windows though its declaration does not.
package m

// Finish ends the process on windows and returns everywhere else.
func Finish() {
	platformFinish()
}
