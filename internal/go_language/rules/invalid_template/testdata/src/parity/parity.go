// Package parity holds every way the query and its filters accept or refuse a call: each template package's Parse on
// a template its New makes in the call, and on one made by Must, Delims, Funcs, a variable or a parenthesized New, a
// method value and a method expression, a function named Parse, the template as a constant written in each way, and
// each kind of error the template packages give, the syntax errors the original reports and the others it leaves.
package parity

import (
	htmltemplate "html/template"
	"text/template"
)

type text string

const (
	broken         = `{{.Name}} {{.LastName}`
	typed     text = `{{end}}`
	undefined      = `{{missing}}`
	valid          = `{{.Name}}`
)

var made = template.Must(template.New("").Parse(`{{end}}`))

func Parse(text string) {}

func errorsOfEachKind() {
	template.New("").Parse(broken)
	template.New("").Parse(`{{end}}`)
	template.New("").Parse(`{{ # }}`)
	template.New("").Parse(`{{.Name | }}`)
	template.New("").Parse(`{{if}}{{end}}`)
	template.New("").Parse(`{{`)
	template.New("").Parse(`{{.Name`)
	template.New("").Parse(undefined)
	template.New("").Parse(`{{range}}`)
	template.New("").Parse(`{{define "a"}}`)
	template.New("").Parse(`{{else}}`)
	template.New("").Parse(`{{(1}}`)
	template.New("").Parse(valid)
	template.New("").Parse("")
	htmltemplate.New("").Parse(broken)
	htmltemplate.New("").Parse(`{{end}}`)
	htmltemplate.New("").Parse(`{{ # }}`)
	htmltemplate.New("").Parse(undefined)
	htmltemplate.New("").Parse(`<a href="{{.URL}}">`)
}

func receivers(other *template.Template) {
	template.New("named").Parse(broken)
	template.Must(template.New(""), nil).Parse(broken)
	template.New("").Delims("[[", "]]").Parse(broken)
	template.New("").Funcs(nil).Parse(broken)
	(template.New("")).Parse(broken)
	other.Parse(broken)
	local := template.New("")
	local.Parse(broken)
	parse := template.New("").Parse
	parse(broken)
	(*template.Template).Parse(template.New(""), broken)
	Parse(broken)
	other.New("").Parse(broken)
	_ = made
}

func constantShapes(value string, condition bool) {
	template.New("").Parse(string(typed))
	template.New("").Parse((broken))
	template.New("").Parse("{{.Name}} " + "{{.LastName}")
	template.New("").Parse(value)
	template.New("").Parse(value + broken)
	chosen := broken
	if condition {
		chosen = valid
	}
	template.New("").Parse(chosen)
}

func callShapes() {
	defer template.New("").Parse(broken)
	go template.New("").Parse(broken)
	func() {
		template.New("").Parse(broken)
	}()
	if _, parseError := template.New("").Parse(broken); parseError != nil {
		return
	}
	_ = template.Must(template.New("").Parse(broken))
}

func generic[T any](value T) T {
	template.New("").Parse(broken)
	return value
}

func instantiated() {
	generic(1)
}
