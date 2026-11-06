package textrules

import (
	"encoding/json"
	"path"
)

// Case is one case, with its sources.
type Case struct {
	Family string
	Name   string
	Why    string

	// Control marks the family's positive control: a case every engine with the family's rule must report,
	// whose count and start lines can never be ledgered.
	Control bool

	// Expect is every finding the rule must report, in source order. Empty means clean, which binds as hard
	// as a reported case does.
	Expect []Span

	// Options is the case's options in one neutral shape, which each adapter translates into its engine's
	// own form over the options the house sets give the rule.
	Options map[string]json.RawMessage

	// Path is the case source's project-relative file name without its extension, which each language adds:
	// `case` unless the case says otherwise, `utils/case` for a rule about folders, `lf02.generated` for one
	// about names.
	Path string

	// Root is the absolute directory the project is checked out at: `/project` unless the case says otherwise,
	// for a rule that could mistake a directory above the project for one inside it. Nothing is written there;
	// an adapter only names the file by it.
	Root string

	Languages []Engine
	Sources   map[Engine]string
}

// Id names the case as `<family>/<case>`.
func (testCase Case) Id() string {
	return testCase.Family + "/" + testCase.Name
}

// Has reports whether the case has a form in an engine's language.
func (testCase Case) Has(engine Engine) bool {
	_, has := testCase.Sources[engine]
	return has
}

// FileName is the case source's project-relative file name for an engine: `case.ts`, or `utils/case.ts`
// under the Path `utils/case`.
func (testCase Case) FileName(engine Engine) string {
	return testCase.Path + "." + extensionOf[engine]
}

// AbsoluteFileName is the case source's file name for an engine under the project's Root.
func (testCase Case) AbsoluteFileName(engine Engine) string {
	return path.Join(testCase.Root, testCase.FileName(engine))
}
