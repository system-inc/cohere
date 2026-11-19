package audit

import "database/sql"

func target(db *sql.DB, text string) {
	_, _ = db.Exec(other + text + "'")
}
