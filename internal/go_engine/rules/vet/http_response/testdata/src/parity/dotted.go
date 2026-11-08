package parity

import (
	"log"
	. "net/http"
)

// A dot-imported function is called by a name, not a selector, and is passed over, while a method of the client is
// judged. A defer rooted in a dot-imported name is another name than the response's; differences.go holds that case,
// which the original reports.
func dotImported(url string) {
	response, err := Get(url)
	defer response.Body.Close()
	client, err := DefaultClient.Get(url)
	defer client.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}
