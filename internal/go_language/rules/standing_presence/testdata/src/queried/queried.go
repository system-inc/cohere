// Package queried queries its rows only in query, which the test strips.
package queried

import "database/sql"

var database *sql.DB

var rows *sql.Rows

func query() { rows, _ = database.Query("select 1") }
