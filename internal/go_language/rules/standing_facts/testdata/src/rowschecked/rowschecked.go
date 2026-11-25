// Package rowschecked is sql_rows_err's stripped fixture in presence's form: package-level rows walked in one body,
// next, and their error checked in another, check.
package rowschecked

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
