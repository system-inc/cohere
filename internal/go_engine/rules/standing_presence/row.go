package standing_presence

import (
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// Row is one rule whose reads presence keeps, by the rule's own name as its findings carry it, so a body's reason for
// leaving reads as the rule that needed it present. The names are vet's and cohere's rule names, an outside grammar,
// so they keep its casing.
type Row string

// HostPort is vet's hostport: a package-level variable whose sole value formats an address is judged by every body
// that passes it to a dial.
const HostPort Row = "hostport"

// Rows are every row presence judges, in the order it judges them.
var Rows = []Row{HostPort}

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
