// Package panics holds the call where the original panics, a Parse whose method is parenthesized, which its query
// matches and its assertion that the function is a selector refuses, and a plain call beside it, which the original
// loses with the package. The port finds both.
package panics

import "text/template"

// panicking holds the records, each ruled in this doc comment: both templates end with no action open, a syntax error
// the original reports on the plain call wherever it runs, and the port rightly finds it on both calls here, the
// parenthesized one the original panics on and the plain one it loses with the package.
func panicking() {
	(template.New("").Parse)(`{{end}}`) // port finds: template: :1: unexpected {{end}}
	template.New("").Parse(`{{end}}`)   // port finds: template: :1: unexpected {{end}}
}
