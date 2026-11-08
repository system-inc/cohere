// Package indirect calls net/http's client through package responses and never imports net/http, so both sides pass
// it over without a walk, the defer that follows the call before its error is checked included.
package indirect

import (
	"log"

	"responses"
)

func throughAnotherPackage() {
	response, err := responses.Client.Get("http://foo.com")
	defer response.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
}
