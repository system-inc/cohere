package parity

import (
	"log"
	nethttp "net/http"

	http "responses"
)

// A qualified call is judged by the name its package is imported under: responses imported as http is taken for
// net/http, and net/http imported as another name is passed over. An instantiation is not a selector, and is passed
// over too.
func importedNames(url string) {
	response, err := http.Get(url)
	defer response.Body.Close() // reported
	renamed, err := nethttp.Get(url)
	defer renamed.Body.Close()
	instantiated, err := http.Fetch[int](url)
	defer instantiated.Body.Close()
	client, err := http.Client.Get(url)
	defer client.Body.Close() // reported
	if err != nil {
		log.Fatal(err)
	}
}
