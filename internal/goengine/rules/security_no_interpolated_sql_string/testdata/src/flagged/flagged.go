package flagged

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Email is a named string, which holds any text a string does.
type Email string

// store embeds a *sql.DB, whose methods are still database/sql's when called on the store.
type store struct {
	*sql.DB
}

func sprintfInsideQuotes(database *sql.DB, name string) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name))
}

func concatenatedInsideQuotes(database *sql.DB, name string) {
	// want +1 "single quotes"
	_, _ = database.Exec("DELETE FROM users WHERE name = '" + name + "'")
}

func everyReceiverAndMethod(requestContext context.Context, database *sql.DB, transaction *sql.Tx, connection *sql.Conn,
	name string) {
	// want +1 "single quotes"
	_ = database.QueryRow("SELECT id FROM users WHERE name = '" + name + "'")
	// want +1 "single quotes"
	_, _ = database.QueryContext(requestContext, "SELECT id FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_ = database.QueryRowContext(requestContext, "SELECT id FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_, _ = database.ExecContext(requestContext, "DELETE FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_, _ = database.Prepare("SELECT id FROM users WHERE name = '" + name + "'")
	// want +1 "single quotes"
	_, _ = database.PrepareContext(requestContext, "SELECT id FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_, _ = transaction.Exec("DELETE FROM users WHERE name = '" + name + "'")
	// want +1 "single quotes"
	_, _ = transaction.QueryContext(requestContext, "SELECT id FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_, _ = connection.ExecContext(requestContext, "DELETE FROM users WHERE name = '"+name+"'")
	// want +1 "single quotes"
	_ = connection.QueryRowContext(requestContext, "SELECT id FROM users WHERE name = '"+name+"'")
}

func embeddedDatabase(users store, name string) {
	// want +1 "single quotes"
	_, _ = users.Query("SELECT id FROM users WHERE name = '" + name + "'")
}

func likePattern(database *sql.DB, word string) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name LIKE '%%%s%%'", word))
}

func lowercaseSql(database *sql.DB, name string) {
	// The call already says this is SQL, so lowercase keywords are read as SQL too.
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("select id from users where name = '%s'", name))
}

func bareAfterComparison(database *sql.DB, identifier string) {
	// want +1 "without quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT name FROM users WHERE id = %s", identifier))
	// want +1 "without quotes"
	_, _ = database.Query("SELECT name FROM users WHERE id >= " + identifier + " LIMIT 10")
}

func throughALocal(database *sql.DB, name string) {
	// want +1 "single quotes"
	query := fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name)
	_, _ = database.Query(query)
	// The same local reaches a second call, and its one mistake is reported once.
	_ = database.QueryRow(query)
}

func throughALocalSum(database *sql.DB, name string) {
	condition := "name = '" + name + "'" // want "single quotes"
	_, _ = database.Query("SELECT id FROM users WHERE " + condition)
}

func sprintfInsideSum(database *sql.DB, name string) {
	// want +1 "single quotes"
	_, _ = database.Query("SELECT id FROM users " + fmt.Sprintf("WHERE name = '%s'", name))
}

func sumInsideSprintf(database *sql.DB, name string) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE %s", "name = '"+name+"'"))
}

func valuesOfEveryTextType(database *sql.DB, address Email, anything any, failure error) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE email = '%s'", address))
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE note = '%v'", anything))
	// want +1 "single quotes"
	_, _ = database.Exec(fmt.Sprintf("INSERT INTO failures (message) VALUES ('%v')", failure))
}

func openTypeParameter[Value any](database *sql.DB, value Value) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%v'", value))
}

func stringTypeParameter[Value ~string | ~int](database *sql.DB, value Value) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%v'", value))
}

func quotesDoubledAlone(database *sql.DB, name string) {
	// Doubling the quote alone is correct on SQLite and open on MySQL, where a backslash also escapes.
	escaped := strings.ReplaceAll(name, "'", "''")
	// want +1 "single quotes"
	_, _ = database.Query("SELECT id FROM users WHERE name = '" + escaped + "'")
}

func formattedWithFlags(database *sql.DB, name string) {
	// A width pads the string and still prints all of it.
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%-20s'", name))
	// A %q wraps the string in double quotes and leaves its single quotes alone.
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%q'", name))
}

func multiByteText(database *sql.DB, name string) {
	// want +1 "single quotes"
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '東京 %s' AND city = 'Zürich'", name))
}

func strconvThatQuotesText(database *sql.DB, name string) {
	// strconv.Quote returns the text it was given, in Go's double quotes, so it is no number.
	// want +1 "without quotes"
	_, _ = database.Query("SELECT id FROM users WHERE name = " + strconv.Quote(name))
}

func valueBeforeADotOutsideAQualifier(database *sql.DB, name string) {
	// A dot after a space is not a qualifier written onto the hole.
	// want +1 "without quotes"
	_, _ = database.Query("SELECT id FROM users WHERE name = " + name + " . ")
}
