package two_files

// second holds its calls on other lines than first's, so the first file's index, asked here, would leave bare each
// call this file excuses and excuse the bare one, whose line the first file's marker sits directly above.
func second() {
	target() // Checked: the second file's trailing reason. // want `^a reason excuses it$`

	target() // want `^nothing excuses it$`

	// Checked: the second file's reason.
	target() // want `^a reason excuses it$`
}
