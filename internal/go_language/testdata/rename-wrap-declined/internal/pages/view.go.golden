// Package pages renders a page from an embedded file, whose text calls LinkURL and reads PageID by name, so both keep
// their names though no Go literal spells them.
package pages

import (
	"embed"
	"html/template"
	"strings"
)

//go:embed page.html
var files embed.FS

// view is the value the page's template executes.
type view struct {
	PageID string
}

// LinkURL is the page's link, which the template calls by name.
func (view) LinkURL() string {
	return "/"
}

// Render renders the page.
func Render() (string, error) {
	page, parseError := template.ParseFS(files, "page.html")
	if parseError != nil {
		return "", parseError
	}
	var rendered strings.Builder
	renderError := page.Execute(&rendered, view{PageID: "first"})
	return rendered.String(), renderError
}
