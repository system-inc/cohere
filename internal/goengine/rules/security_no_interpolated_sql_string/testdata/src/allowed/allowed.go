package allowed

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const usersTable = "users"

func boundParameters(requestContext context.Context, database *sql.DB, name string) {
	_, _ = database.Query("SELECT id FROM users WHERE name = ?", name)
	_, _ = database.ExecContext(requestContext, "DELETE FROM users WHERE name = $1", name)
}

func builtOnlyFromConstants(database *sql.DB, name string) {
	_, _ = database.Query("SELECT id FROM "+usersTable+" WHERE name = ?", name)
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM %s WHERE name = '%s'", usersTable, "admin"))
}

func namesAndClausesFromCode(database *sql.DB, table string, column string, condition string, placeholders []string) {
	// A table, a column, a condition or a list of placeholders is SQL built from code, where no parameter can go.
	_, _ = database.Query(fmt.Sprintf("SELECT %s FROM %s WHERE name = ?", column, table), "admin")
	_, _ = database.Query("SELECT id FROM users WHERE active AND "+condition, "admin")
	_, _ = database.Query("SELECT id FROM users WHERE id IN ("+strings.Join(placeholders, ", ")+")", 1, 2)
	_, _ = database.Exec(fmt.Sprintf("CREATE DATABASE %s", table))
	_, _ = database.Exec("DROP TABLE " + table)
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users ORDER BY %s", column))
}

func namesInsideQuotes(database *sql.DB, table string, column string) {
	// Double quotes and backquotes hold a name, not a value.
	_, _ = database.Query(fmt.Sprintf(`SELECT "%s" FROM users`, column))
	_, _ = database.Query("SELECT id FROM `" + table + "` WHERE name = ?")
}

func numbersAndBooleans(database *sql.DB, limit int, identifier int64, ratio float64, active bool) {
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users LIMIT %d", limit))
	_, _ = database.Query(fmt.Sprintf("SELECT name FROM users WHERE id = %d", identifier))
	_, _ = database.Query(fmt.Sprintf("SELECT name FROM users WHERE id = '%d'", identifier))
	_, _ = database.Query(fmt.Sprintf("SELECT name FROM users WHERE ratio > %v AND active = %t", ratio, active))
	_, _ = database.Exec(fmt.Sprintf("PRAGMA cache_size = -%d", limit))
}

func numericTypeParameter[Number int | int64 | float64](database *sql.DB, value Number) {
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE score = '%v'", value))
}

func hexDigits(database *sql.DB, name string) {
	// %x prints hex digits whatever the string holds.
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name_hex = '%x'", name))
}

func escapedBothWays(database *sql.DB, name string) {
	// Backslashes and quotes both doubled: no input closes the string on SQLite, standard SQL or MySQL.
	escaped := strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), "'", "''")
	_, _ = database.Query("SELECT id FROM users WHERE name = '" + escaped + "'")
	_, _ = database.Query(fmt.Sprintf(
		"SELECT id FROM users WHERE name = '%s'",
		strings.Replace(strings.Replace(name, "'", "''", -1), `\`, `\\`, -1),
	))
}

func uncertainReading(database *sql.DB, name string) {
	// A backslash in quoted text is an escape on MySQL and a character elsewhere, so where the string ends is not
	// known and the query is not read.
	_, _ = database.Query(fmt.Sprintf(`SELECT id FROM users WHERE path = 'C:\%s'`, name))
	// # opens a comment on MySQL only.
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users # by name\nWHERE name = '%s'", name))
}

func unreadableFormats(database *sql.DB, name string, format string, values []any) {
	// An explicit index, a non-constant format or a spread argument list hides which value each verb prints.
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%[1]s'", name))
	_, _ = database.Query(fmt.Sprintf(format, name))
	_, _ = database.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", values...))
}

func preparedStatement(requestContext context.Context, statement *sql.Stmt, name string) {
	// A *sql.Stmt takes only arguments, which are bound whatever they hold.
	_, _ = statement.Exec(fmt.Sprintf("'%s'", name))
	_ = statement.QueryRowContext(requestContext, "'"+name+"'")
}

func queryHandedIn(database *sql.DB, query string) {
	// A query handed in whole is not built here.
	_, _ = database.Query(query)
}
