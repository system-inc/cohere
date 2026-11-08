// Package parity holds every branch of sqlrowserr's judgment and the shapes most likely to set the port apart from
// the original, each finding beside a clean case. The comments say what each side should do; the test compares the
// two sides, not these comments.
package parity

import (
	"context"
	"database/sql"
	sqlpackage "database/sql"
)

// Each query method, reported when its rows meet a Next loop with no Err.

func eachMethod(connection *sql.Conn, database *sql.DB, statement *sql.Stmt, transaction *sql.Tx) {
	{
		rows, _ := connection.QueryContext(context.Background(), "") // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := database.QueryContext(context.Background(), "") // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := database.Query("") // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := statement.QueryContext(context.Background()) // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := statement.Query() // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := transaction.QueryContext(context.Background(), "") // reported
		for rows.Next() {
		}
	}
	{
		rows, _ := transaction.Query("") // reported
		for rows.Next() {
		}
	}
}

// Methods that are not one of the seven, silent.

func otherMethods(database *sql.DB, connection *sql.Conn) {
	row := database.QueryRow("") // silent: one row, no Next loop
	_ = row.Scan()
	statement, _ := database.Prepare("") // silent: not a query
	_ = statement.Close()
	_ = connection.Close()
}

// The declaration forms.

func declared(database *sql.DB) {
	var rows, _ = database.Query("") // reported: var with values
	for rows.Next() {
	}
}

func declaredWithType(database *sql.DB) {
	var rows, queryError = database.Query("") // reported
	_ = queryError
	for rows.Next() {
	}
}

func assignedNotDeclared(database *sql.DB) {
	var rows *sql.Rows
	rows, _ = database.Query("") // silent: the left side is a use of rows, which is not a method call
	for rows.Next() {
	}
}

func redeclared(database *sql.DB) {
	rows, firstError := database.Query("") // silent: rows is used again on the left of the second query
	_ = firstError
	for rows.Next() {
	}
	rows, secondError := database.Query("") // silent: the left side reuses rows
	_ = secondError
	for rows.Next() {
	}
}

func blank(database *sql.DB) {
	_, queryError := database.Query("") // silent: no variable
	_ = queryError
}

type holder struct {
	rows *sql.Rows
}

func intoField(database *sql.DB, held *holder) {
	held.rows, _ = database.Query("") // silent: a field, not a variable
	for held.rows.Next() {
	}
}

func intoElement(database *sql.DB, all []*sql.Rows) {
	all[0], _ = database.Query("") // silent: an element
	for all[0].Next() {
	}
}

func parenthesized(database *sql.DB) {
	rows, _ := (database.Query("")) // silent: the call's parent is its parentheses
	for rows.Next() {
	}
}

var packageRows, packageError = packageDatabase.Query("") // reported: package-level, iterated in a later function

var packageDatabase *sql.DB

func iteratePackageRows() {
	for packageRows.Next() {
	}
}

// Where Next is called.

func nextOutsideLoop(database *sql.DB) {
	rows, _ := database.Query("") // silent: Next outside any loop
	if rows.Next() {
		_ = rows.Scan()
	}
}

func loopBeforeDeclaration(database *sql.DB) {
	for {
		rows, _ := database.Query("") // silent: the innermost loop begins before rows
		if !rows.Next() {
			break
		}
	}
}

func innerLoopAfterDeclaration(database *sql.DB) {
	for {
		rows, _ := database.Query("") // reported: the innermost loop begins after rows
		for rows.Next() {
		}
		break
	}
}

func rangeLoop(database *sql.DB) {
	rows, _ := database.Query("") // reported: a range loop
	for range 3 {
		rows.Next()
	}
}

func rangeOverFunction(database *sql.DB) {
	rows, _ := database.Query("") // reported: a range loop over a function
	for range func(yield func() bool) { yield() } {
		rows.Next()
	}
}

func initialStatement(database *sql.DB) {
	rows, _ := database.Query("") // reported: Next in the loop's init and post
	for more := rows.Next(); more; more = rows.Next() {
	}
}

func lastNext(database *sql.DB) {
	rows, _ := database.Query("") // reported at the line of the last Next in a loop
	for rows.Next() {
	}
	rows.Next()
	for {
		if !rows.Next() {
			break
		}
	}
}

func nestedInSwitch(database *sql.DB, choice int) {
	rows, _ := database.Query("") // reported
	for {
		switch choice {
		case 1:
			if !rows.Next() {
				return
			}
		}
	}
}

func closureInLoop(database *sql.DB) {
	rows, _ := database.Query("") // reported: a closure in a loop is in the loop
	for index := 0; index < 3; index++ {
		func() {
			rows.Next()
		}()
	}
}

func loopInClosure(database *sql.DB) {
	rows, _ := database.Query("") // reported
	run := func() {
		for rows.Next() {
		}
	}
	run()
}

func goroutine(database *sql.DB) {
	rows, _ := database.Query("") // reported
	go func() {
		for rows.Next() {
		}
	}()
}

func labelled(database *sql.DB) {
	rows, _ := database.Query("") // reported
rowsLoop:
	for rows.Next() {
		break rowsLoop
	}
}

// What silences it.

func errChecked(database *sql.DB) error {
	rows, _ := database.Query("") // silent
	for rows.Next() {
	}
	return rows.Err()
}

func errBeforeLoop(database *sql.DB) {
	rows, _ := database.Query("") // silent: Err is called, wherever
	_ = rows.Err()
	for rows.Next() {
	}
}

func errInClosure(database *sql.DB) {
	rows, _ := database.Query("") // silent
	for rows.Next() {
	}
	defer func() { _ = rows.Err() }()
}

func errMethodValue(database *sql.DB) {
	rows, _ := database.Query("") // silent: a method value escapes
	for rows.Next() {
	}
	check := rows.Err
	_ = check
}

func nextMethodValue(database *sql.DB) {
	rows, _ := database.Query("") // silent: a method value escapes
	next := rows.Next
	for next() {
	}
}

func parenthesizedReceiver(database *sql.DB) {
	rows, _ := database.Query("") // silent: (rows) is not a selector's operand
	for (rows).Next() {
	}
}

func parenthesizedSelector(database *sql.DB) {
	rows, _ := database.Query("") // silent: (rows.Next) is not a call's function
	for (rows.Next)() {
	}
}

func passedOn(database *sql.DB) {
	rows, _ := database.Query("") // silent: passed to a function
	for rows.Next() {
	}
	consume(rows)
}

func returned(database *sql.DB) *sql.Rows {
	rows, _ := database.Query("") // silent: returned
	for rows.Next() {
	}
	return rows
}

func compared(database *sql.DB) {
	rows, _ := database.Query("") // silent: compared with nil
	if rows == nil {
		return
	}
	for rows.Next() {
	}
}

func deferredClose(database *sql.DB) {
	rows, _ := database.Query("") // reported: Close and Scan are method calls, which keep it a candidate
	defer rows.Close()
	for rows.Next() {
		var value int
		_ = rows.Scan(&value)
	}
	_, _ = rows.Columns()
}

func consume(*sql.Rows) {}

// How the query is reached.

type embedding struct {
	*sql.DB
}

func embedded(wrapped embedding) {
	rows, _ := wrapped.Query("") // reported: a promoted method is the method
	for rows.Next() {
	}
}

type genericEmbedding[T any] struct {
	*sql.DB
	value T
}

func genericEmbedded(wrapped genericEmbedding[int]) {
	rows, _ := wrapped.Query("") // reported: promoted through an instance of a generic type
	for rows.Next() {
	}
}

func generic[T any](database *sql.DB, value T) T {
	rows, _ := database.Query("") // reported: inside a generic function
	for rows.Next() {
	}
	return value
}

func methodExpression(database *sql.DB) {
	rows, _ := (*sql.DB).Query(database, "") // reported: a method expression calls the method
	for rows.Next() {
	}
}

func parenthesizedFunction(database *sql.DB) {
	rows, _ := (database.Query)("") // reported: parentheses around the function
	for rows.Next() {
	}
}

type databaseAlias = sql.DB

func aliased(database *databaseAlias) {
	rows, _ := database.Query("") // reported: an alias names the same type
	for rows.Next() {
	}
}

func renamedImport(database *sqlpackage.DB) {
	rows, _ := database.Query("") // reported: the import's name does not matter
	for rows.Next() {
	}
}

type querier interface {
	Query(query string, arguments ...any) (*sql.Rows, error)
}

func throughInterface(database querier) {
	rows, _ := database.Query("") // silent: an interface's method, not database/sql's
	for rows.Next() {
	}
}

type lookalike struct{}

func (lookalike) Query(string) (*sql.Rows, error) { return nil, nil }

func ownQuery(database lookalike) {
	rows, _ := database.Query("") // silent: a method of this package
	for rows.Next() {
	}
}

func shadowed(database *sql.DB) {
	rows, _ := database.Query("") // reported: the inner rows is another variable
	for rows.Next() {
	}
	{
		rows := 0
		_ = rows
	}
}

func shadowedErr(database *sql.DB) {
	rows, _ := database.Query("") // silent: the inner rows' Err is still a call of rows.Err
	for rows.Next() {
	}
	{
		rows, _ := database.Query("") // reported: its own Next loop, no Err of its own
		for rows.Next() {
		}
	}
	_ = rows.Err()
}
