package parity

import (
	"log"
	. "net/http"
)

// A dot-imported function is called by a name, not a selector, and is passed over, while a method of the client is
// judged. The parser leaves a dot import's names unresolved, so a defer rooted in one counts as one with a response
// that is unresolved too.
func dotImported(url string) {
	response, err := Get(url)
	defer response.Body.Close()
	client, err := DefaultClient.Get(url)
	defer client.Body.Close() // reported
	otherFile, err = DefaultClient.Get(url)
	defer DefaultClient.CloseIdleConnections() // reported
	if err != nil {
		log.Fatal(err)
	}
}
