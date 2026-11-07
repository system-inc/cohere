package global_rename

import (
	"strings"
	"testing"
)

// A template parsed from an embedded file calls its value's method and reads its field by the names the file spells,
// which no Go literal holds: both keep their names, the method for its receiver reaching a template and the field as
// the encoding guard has always kept a field a template reads.
func TestAMethodAndAFieldATemplateFileReadsAreRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/page.html": "{{.LinkURL}} {{.PageID}}\n",
		"internal/a/a.go": `package a

import (
	"embed"
	"html/template"
	"strings"
)

//go:embed page.html
var pages embed.FS

type view struct{ PageID string }

func (view) LinkURL() string { return "/" }

func Render() string {
	var rendered strings.Builder
	page, parseError := template.ParseFS(pages, "page.html")
	if parseError == nil {
		_ = page.Execute(&rendered, view{PageID: "p"})
	}
	return rendered.String()
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "LinkURL", Kind: KindMethod, Owner: "view", NewName: "LinkUrl"},
		target{PackageSuffix: "internal/a", Name: "PageID", Kind: KindField, NewName: "PageId"},
	), Options{HostOnly: true})
	requireRefused(test, result, "LinkURL", "encoding guard")
	requireRefused(test, result, "PageID", "encoding guard")
	for _, rejection := range result.Rejections {
		if rejection.OldName == "LinkURL" && !strings.Contains(rejection.Reason, "reaches a template") {
			test.Errorf("LinkURL is refused as %q, want its receiver reaching a template", rejection.Reason)
		}
	}
}

// reflect's MethodByName with a name the caller computes calls a method no literal spells, so a method of a type whose
// values reach reflect keeps its name while a workspace package makes such a lookup. A lookup by a constant that names
// another method computes nothing, and the method renames.
func TestAMethodAComputedReflectLookupCanCallIsRefused(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name     string
		argument string
		refused  bool
	}{
		{name: "a computed name", argument: "name", refused: true},
		{name: "a constant naming another method", argument: `"Other"`, refused: false},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newFixture(test, map[string]string{
				"internal/a/a.go": `package a

import "reflect"

type handler struct{}

func (handler) RunHTTP() {}

func (handler) Other() {}

func Call(target handler, name string) {
	_ = name
	reflect.ValueOf(target).MethodByName(` + testCase.argument + `).Call(nil)
}
`,
			})
			loaded := fixture.load(test)
			result := Plan(loaded, propose(test, loaded,
				target{PackageSuffix: "internal/a", Name: "RunHTTP", Kind: KindMethod, Owner: "handler",
					NewName: "RunHttp"},
			), Options{HostOnly: true})
			if testCase.refused {
				requireRefused(test, result, "RunHTTP", "encoding guard")
				return
			}
			requireAccepted(test, result, 1)
		})
	}
}
