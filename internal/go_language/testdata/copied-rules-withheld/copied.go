// cohere:copied example.com/upstream@v1.0.0 report/report.go
// cohere:until never: a planted copy, which the copied-rules-withheld case runs the engine over.

// Package copied is a copy of upstream source. The house rule the case runs reports on every file, and its finding
// here is withheld and counted; vet's printf finding below still reports, since a vet finding in a copy is still a
// bug in what ships.
package copied

import "fmt"

// Report formats a count with a verb that does not fit it.
func Report() string {
	return fmt.Sprintf("%d", "three")
}
