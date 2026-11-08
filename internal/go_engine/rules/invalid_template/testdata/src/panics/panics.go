// Package panics holds the call where the original panics, a Parse whose method is parenthesized, which its query
// matches and its assertion that the function is a selector refuses, and a plain call beside it, which the original
// loses with the package. The port finds both.
package panics

import "text/template"

func panicking() {
	(template.New("").Parse)(`{{end}}`) // port finds: template: :1: unexpected {{end}}
	template.New("").Parse(`{{end}}`)   // port finds: template: :1: unexpected {{end}}
}
