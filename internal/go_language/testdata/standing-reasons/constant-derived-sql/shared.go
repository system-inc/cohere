package audit

import "database/sql"

func target(db *sql.DB, text string) {
	_, _ = db.Exec(other + text + "'")
}

// other is derived from base, which each build declares, so its value differs where nothing of its own does.
const other = base + " '"
