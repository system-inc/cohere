package differences

import "net/http"

// otherFile is declared apart from where differences.go and dotted.go assign it, so the parser leaves its uses there
// unresolved.
var otherFile *http.Response
