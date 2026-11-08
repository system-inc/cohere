package differences

import (
	"log"
	. "net/http"
)

func dotImported(url string) {
	var err error
	otherFile, err = DefaultClient.Get(url)
	// DefaultClient is net/http's client, dot-imported, and closing its idle connections uses no response.
	defer DefaultClient.CloseIdleConnections() // port drops: using otherFile before checking for errors
	if err != nil {
		log.Fatal(err)
	}
}
