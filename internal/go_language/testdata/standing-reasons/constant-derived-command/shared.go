package audit

import (
	"database/sql"
	"fmt"
	"os/exec"
)

func target(db *sql.DB, text string, err error) error {
	_ = exec.Command("go", "test", other)
	_, _ = db.Exec("SELECT " + other + " " + text)
	return fmt.Errorf(other, err)
}

// other is derived from base, which each build declares, so its value differs where nothing of its own does.
const other = base + "1"
