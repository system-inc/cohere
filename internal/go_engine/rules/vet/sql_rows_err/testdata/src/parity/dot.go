package parity

import (
	. "database/sql"
)

// A dot import names the types unqualified, and their methods are the same methods.

func dotImported(database *DB) {
	rows, _ := database.Query("") // reported
	for rows.Next() {
	}
}

func dotImportedChecked(transaction *Tx) error {
	rows, _ := transaction.Query("") // silent
	for rows.Next() {
	}
	return rows.Err()
}
