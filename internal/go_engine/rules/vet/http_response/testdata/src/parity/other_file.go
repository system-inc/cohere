package parity

import "net/http"

// otherFile is declared apart from where parity.go assigns it, so the parser leaves its uses there unresolved.
var otherFile *http.Response
