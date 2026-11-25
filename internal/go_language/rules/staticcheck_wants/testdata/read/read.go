// Package read holds a note of each shape staticcheck's fixtures write, and comments that hold no note.
package read

func notes() {
	_ = 1 //@ diag(`plain text`)
	_ = 2 //@ diag(re`pattern \d+`)
	_ = 3 //@ diag("first"), diag("second")
	_ = 4 // a comment before the note //@ diag(`inside a comment`)
	_ = 5 //@diag (`no space after the at`)
	/*@ diag(`in a block`) */ _ = 6
	/*@ diag(
		`over two lines`,
	) */
	_ = 7
}

func notNotes() {
	_ = 1 // @ diag(`a space before the at`)
	_ = 2 // written to someone@example.com
	_ = 3 // diag(`no at`)
}
