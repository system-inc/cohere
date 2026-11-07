package two_files

func target() {}

// first holds a call its marker excuses from above, a bare call, and a call its marker excuses from the line's end.
func first() {
	// Checked: the first file's reason.
	target() // want `^a reason excuses it$`
	target() // want `^nothing excuses it$`
	target() // Checked: the first file's trailing reason. // want `^a reason excuses it$`
}
