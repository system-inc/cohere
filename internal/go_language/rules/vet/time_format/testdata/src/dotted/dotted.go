// Package dotted imports time with a dot, in a package of its own, since a dot import puts Parse in the file's scope
// and parity declares a Parse of its own.
package dotted

import . "time"

// A dot import names Parse and Time without the package's name, and they are still time's.
func dotted(moment Time) {
	_, _ = Parse("2006-02-01", "2021-01-01")
	_ = moment.Format("2006-02-01")
	_ = Now().Format(RFC3339)
	_ = Unix(0, 0).Format(DateOnly)
}
