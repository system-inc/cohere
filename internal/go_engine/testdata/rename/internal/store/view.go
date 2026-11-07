package store

import (
	"strings"
	"text/template"
)

// view is rendered by a template that calls its method by name, so the method keeps its name.
type view struct{}

// TitleHTML is the page's title, which the template below calls as .TitleHTML.
func (view) TitleHTML() string {
	return "<h1>store</h1>"
}

// Render renders the view through a template.
func Render() (string, error) {
	page, parseError := template.New("page").Parse("{{.TitleHTML}}")
	if parseError != nil {
		return "", parseError
	}
	var rendered strings.Builder
	renderError := page.Execute(&rendered, view{})
	return rendered.String(), renderError
}
