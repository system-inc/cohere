// Package stripped walks package-level rows in one body and checks their error in another, which the strip takes
// away.
package stripped

import "database/sql"

var database *sql.DB

var rows, queryError = database.Query("select 1")

// next loops on the rows and never checks their error.
func next() {
	for rows.Next() {
	}
}

// check is where the rows' error is checked, and the strip takes its body.
func check() error { return rows.Err() }
