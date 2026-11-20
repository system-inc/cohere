package gate

import "database/sql"

// database is the handle the query goes through.
var database *sql.DB

// rows is sqlrowserr's candidate, declared in the file both builds compile.
var rows, queryError = database.Query("select 1")

// Next names rows, so sqlrowserr reads it.
func Next() bool { return rows.Next() }

// Port names no candidate.
func Port() int { return 80 }
