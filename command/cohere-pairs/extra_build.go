package main

// extraBuild is the part of one entry of the lint record's builds the table reads, by the names
// internal/go_engine/contract's BuildRecord gives them, its own shape for the reason engineLine is. Each count is
// left out of the record when it is 0, so a build without one reads 0.
type extraBuild struct {
	Target            string `json:"target"`
	Host              bool   `json:"host"`
	CheckedFromSource int    `json:"checkedFromSource"`
	RestoredFromHost  int    `json:"restoredFromHost"`
	DeclinedAtLoad    int    `json:"declinedAtLoad"`
}
