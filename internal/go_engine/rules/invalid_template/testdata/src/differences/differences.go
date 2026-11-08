// Package differences holds the shapes where the port's range could part from the original's: a template on a line
// of its own, beside comments, inside parentheses, joined from constants over lines, and a call spread over lines.
// The port and the original find the same here, so the fixture records no difference.
package differences

import "text/template"

const closing = `}`

func shapes() {
	template.New("").Parse(
		`{{.Name}`,
	)
	template.New("").Parse( /* before */ `{{end}}` /* after */)
	template.New("").Parse(`{{end}}`) // trailing
	template.New("").Parse((`{{end}}`))
	template.New("").Parse(`{{.Name}` +
		closing)
	template.New("").Parse(
		// leading
		`{{ # }}`,
	)
}
