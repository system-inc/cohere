// Package rowing is the build's package the sqlrowserr presence test settles: each variable says beside it whether
// the rule's predicate holds of it.
package rowing

import "database/sql"

// database is the handle the queries go through.
var database *sql.DB

// Rows is sqlrowserr's candidate: the first name a query's rows are assigned to.
var Rows, queryError = database.Query("select 1")

// Prepared is no candidate: a prepared statement is no query's rows.
var Prepared, prepareError = database.Prepare("select 1")

// Next names Rows.
func Next() bool { return Rows.Next() }

// Count names no variable.
func Count() int { return 1 }

// Statement names Prepared.
func Statement() *sql.Stmt { return Prepared }
