package lookalike

import (
	"database/sql"
	"fmt"
)

// recorder has a Query and an Exec of its own, which take text that is not database/sql's query.
type recorder struct{}

func (recorder) Query(text string, values ...any) {}

func (recorder) Exec(text string) {}

// Sprintf is this package's own, so a call to it is not fmt's and its format is not read.
func Sprintf(format string, values ...any) string {
	return format
}

func methodsOfTheSameName(log recorder, name string) {
	log.Query(fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name))
	log.Exec("DELETE FROM users WHERE name = '" + name + "'")
}

func sprintfOfAnotherPackage(database *sql.DB, name string) {
	_, _ = database.Query(Sprintf("SELECT id FROM users WHERE name = '%s'", name))
}

func sqlThatIsNotAQuery(name string) {
	fmt.Println(fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name))
	message := "DELETE FROM users WHERE name = '" + name + "'"
	fmt.Println(message)
}

func reassignedBeforeTheCall(database *sql.DB, name string) {
	// A local assigned twice is not read in place, since which value reaches the call depends on the path.
	query := fmt.Sprintf("SELECT id FROM users WHERE name = '%s'", name)
	fmt.Println(query)
	query = "SELECT id FROM users"
	_, _ = database.Query(query)
}

func valueAfterTheQuery(database *sql.DB, name string) {
	// The values after the query are bound, whatever they are built from.
	_, _ = database.Query("SELECT id FROM users WHERE name = ?", "'"+name+"'")
}
