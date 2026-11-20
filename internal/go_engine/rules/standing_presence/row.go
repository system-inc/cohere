package standing_presence

import (
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Row is one rule whose reads presence keeps, by the name the stage 5 census writes it under (#xeafx79), so a body's
// reason for leaving reads as the row that needed it present: vet's pass names for its rules, and a short name of
// cohere's own rule for the rest. The names are an outside grammar, vet's and the census's, so they keep its casing.
type Row string

const (
	// HostPort is vet's hostport: a package-level variable whose sole value formats an address is judged by every body
	// that passes it to a dial.
	HostPort Row = "hostport"
	// ScannerError is vet's scannererr: a package-level variable a scanner is assigned to is judged by every body that
	// calls a method on it.
	ScannerError Row = "scannererr"
	// SqlRowsError is vet's sqlrowserr: a package-level variable a query's rows are assigned to is judged by every
	// body that calls a method on it.
	SqlRowsError Row = "sqlrowserr"
	// Screaming is consistency-no-screaming-snake-case: an underscored constant of a system file is exempt when a
	// comparison in any body ties it to the system constant of its name.
	Screaming Row = "screaming"
	// EmptyFunction is consistency-no-empty-function: a one-method interface's empty methods are markers only while no
	// body selects a method of that name.
	EmptyFunction Row = "emptyfunction"
	// NoAny is consistency-no-any, by its bound: an any or an empty interface written in a declaration's type is fixed
	// by what bodies hand the declaration.
	NoAny Row = "noany"
	// TestingGoroutine is vet's testinggoroutine: a body that starts a function of the package with go has that
	// function's body judged for what a goroutine must not call.
	TestingGoroutine Row = "testinggoroutine"
	// RequireParallel is testing-require-parallel: a (*testing.T).Run handed a function of the package by name has
	// that function's body judged for its first statement.
	RequireParallel Row = "requireparallel"
)

// Rows are every row presence judges, in the order it judges them.
var Rows = []Row{HostPort, ScannerError, SqlRowsError, Screaming, EmptyFunction, NoAny, TestingGoroutine,
	RequireParallel}

// reasonPrefix starts the reason of everything a row takes out, so a removal is read back to its row (RowOf).
const reasonPrefix = "presence "

// Reason is why a body left, or an object was named, for row.
func (row Row) Reason() fact_reach.Reason {
	return fact_reach.Reason(reasonPrefix + string(row))
}

// RowOf is the row reason was given for, and false for a reason no row gives.
func RowOf(reason fact_reach.Reason) (Row, bool) {
	name, isPresence := strings.CutPrefix(string(reason), reasonPrefix)
	return Row(name), isPresence && slices.Contains(Rows, Row(name))
}
