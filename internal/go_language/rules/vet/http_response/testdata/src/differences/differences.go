// Package differences holds every finding the original makes and the port drops, each ruled a false positive, with
// its reason on the line above. The original holds the deferred call's root to the response's name by the parser's
// resolution, under which every name the parser leaves unresolved is nil and so the same as every other; the port
// compares the objects the type checker gives them.
package differences

import (
	"log"
	"net/http"
	"os"
)

func blankAssigned(url string) {
	var err error
	_, err = http.Get(url)
	// `_` assigned with `=` names no variable, and log is a package: nothing deferred uses a response.
	defer log.Println("deferred") // port drops: using _ before checking for errors
	if err != nil {
		log.Fatal(err)
	}
}

func packageLevel(url string) {
	var err error
	otherFile, err = http.Get(url)
	// otherFile is a variable declared in other_file.go, and os is a package: os.Exit uses no response.
	defer os.Exit(1) // port drops: using otherFile before checking for errors
	otherFile, err = http.Get(url)
	// panic is the predeclared function, handed the error, not the response.
	defer panic(err) // port drops: using otherFile before checking for errors
	if err != nil {
		log.Fatal(err)
	}
}
