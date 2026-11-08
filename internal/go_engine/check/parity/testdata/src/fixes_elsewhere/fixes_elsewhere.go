// Package fixes_elsewhere holds a call only the port finds, whose fix edits the declaration of the function called, in
// another file, recorded with where the edit lands.
package fixes_elsewhere

// calls holds the planted record, ruled in this doc comment: the port rightly finds gained, and rightly fixes it where
// gained is declared.
func calls() {
	gained() // port finds: call to gained // port fixes at fixes_elsewhere/declared.go:4: renamed
}
