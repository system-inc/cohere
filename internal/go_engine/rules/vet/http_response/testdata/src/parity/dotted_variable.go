package parity

import (
	"log"
	"net/http"

	. "responses"
)

// A dot-imported variable is left unresolved by the parser, and is the same variable in the defer, so it is reported.
func dotImportedVariable(url string) {
	var err error
	Last, err = http.Get(url)
	defer Last.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}
