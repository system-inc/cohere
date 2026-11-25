package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/types/source_name"
)

// language is one name `--languages` takes (#s7gt4bj): the engine whose projects it checks, if any, and the files
// of its kind the house formatter formats, wherever they live, by extension. Every file a run checks is in exactly
// one language, so a run naming some never checks the rest in silence.
type language struct {
	name       string
	engine     projectEngine
	extensions []string
}

// languages are the names `--languages` takes, in the order a list of them is said. Read without regard to case,
// so `Go` and `go` name one language. `typescript` takes JavaScript too, the programs and the loose files alike, and
// every extension the house formatter registers is one language's (TestEveryFormattedExtensionIsOneLanguages).
var languages = []language{
	{name: "go", engine: engineGo},
	{
		name:   "typescript",
		engine: engineTypeScript,
		extensions: []string{
			".ts", ".tsx", ".mts", ".cts", ".js", ".mjs", ".cjs", ".jsx",
		},
	},
	{name: "swift", engine: engineSwift},
	{name: "adamic", extensions: []string{source_name.AdamicExtension}},
	{name: "json", extensions: []string{".json"}},
	{name: "markdown", extensions: []string{".md"}},
	{name: "yaml", extensions: []string{".yaml", ".yml"}},
	{name: "css", extensions: []string{".css"}},
	{name: "graphql", extensions: []string{".graphql", ".gql"}},
}

// languageChoice is the names of the languages `--languages` named, in languages' order, nil for a run that named
// none, which checks every language it finds.
type languageChoice []string

// parseLanguageChoice reads `--languages`' comma list. An unknown name, an empty list or an empty entry is refused
// with the names it takes, since a run that read a misspelled language as none would check less than was asked and
// say nothing.
func parseLanguageChoice(value string) (languageChoice, error) {
	named := map[string]bool{}
	for entry := range strings.SplitSeq(value, ",") {
		name := strings.ToLower(strings.TrimSpace(entry))
		if !slices.ContainsFunc(languages, func(known language) bool { return known.name == name }) {
			said := "an empty name"
			if name != "" {
				said = name
			}
			return nil, fmt.Errorf("--languages %s names %s, which is no language cohere checks: it takes %s, "+
				"as a comma list, so nothing was checked", value, said, sayList(languageNames(nil)))
		}
		named[name] = true
	}
	return languageNames(func(known language) bool { return named[known.name] }), nil
}

// languageNames is the names of the languages kept says yes to, in languages' order, or of every language when kept
// is nil.
func languageNames(kept func(language) bool) []string {
	names := []string{}
	for _, known := range languages {
		if kept == nil || kept(known) {
			names = append(names, known.name)
		}
	}
	return names
}

// names reports whether the choice names a language: every one, for a run that named none.
func (choice languageChoice) names(name string) bool {
	return choice == nil || slices.Contains(choice, name)
}

// namesEngine reports whether the choice names the language whose projects an engine checks.
func (choice languageChoice) namesEngine(engine projectEngine) bool {
	return slices.ContainsFunc(languages, func(known language) bool {
		return known.engine == engine && choice.names(known.name)
	})
}

// formats reports whether the choice names any language the house formatter formats files of, so a format walk has
// something to do under it.
func (choice languageChoice) formats() bool {
	return slices.ContainsFunc(languages, func(known language) bool {
		return len(known.extensions) > 0 && choice.names(known.name)
	})
}

// leftOutExtensions are the extensions of the files every format walk under the choice leaves out, those of the
// languages it did not name, sorted: none for a run that named none.
func (choice languageChoice) leftOutExtensions() []string {
	var extensions []string
	for _, known := range languages {
		if !choice.names(known.name) {
			extensions = append(extensions, known.extensions...)
		}
	}
	slices.Sort(extensions)
	return extensions
}

// leavesOutFile reports whether a file is in a language the run left out, by its extension, read as discovery and the
// formatter read it, without regard to case.
func leavesOutFile(leftOutExtensions []string, fileName string) bool {
	return slices.Contains(leftOutExtensions, strings.ToLower(filepath.Ext(fileName)))
}

// sayList joins names as a sentence lists them: `go`, `go and swift`, `go, swift and json`, or `none`.
func sayList(names []string) string {
	switch len(names) {
	case 0:
		return "none"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// languageChoiceOf is the choice `--languages` made, nil when the command line did not name it. Read before anything
// else is, so a misspelled language is refused wherever the run stands.
func languageChoiceOf(given map[string]bool, value string) (languageChoice, error) {
	if !given["languages"] {
		return nil, nil
	}
	return parseLanguageChoice(value)
}

// chooseNamedRunLanguages is what a choice does to a run that names its project rather than discovering: it checks
// the project only when the project's language is named, and its format walk leaves out the languages not named, as
// a discovered project's run does through its yield. A run that discovers chooses from what it finds instead
// (chooseLanguages).
func chooseNamedRunLanguages(
	discovers bool,
	choice languageChoice,
	location projectLocation,
	locateError error,
) error {
	if discovers {
		return nil
	}
	if refuseError := refuseUnchosenLanguage(choice, location, locateError); refuseError != nil {
		return refuseError
	}
	namedRunLeftOutExtensions = choice.leftOutExtensions()
	return nil
}

// refuseUnchosenLanguage refuses a run that discovery does not apply to, one given `--directory` into one project, a
// tsconfig or paths, when its project's own language is not named, since a project run there checks that language
// first. A run that discovers, from the root or a directory holding several, checks a named language wherever its
// files live, a TypeScript program's JSON included (chooseLanguages). Nil when it is named, or when no languages were
// named.
func refuseUnchosenLanguage(choice languageChoice, location projectLocation, locateError error) error {
	if locateError != nil || choice.namesEngine(location.Engine) {
		return nil
	}
	// The format project is no language's own: it formats each language's files, which its walk keeps by name.
	if location.Engine == engineFormat && choice.formats() {
		return nil
	}
	return fmt.Errorf("--languages %s does not name %s, the language of the project at %s, so nothing was checked: "+
		"run from the root, which checks the languages named wherever their files live", sayList(choice),
		languageOfEngine(location.Engine), location.Root)
}

// languageOfEngine is the name `--languages` takes for the language an engine checks.
func languageOfEngine(engine projectEngine) string {
	for _, known := range languages {
		if known.engine == engine {
			return known.name
		}
	}
	return string(engine)
}

// languagesJson is what `--languages` chose, as a record of the run's own under `--json`, written once before the
// projects' lines (#s7gt4bj): the languages it named, those of them here that it checked, those here it left out,
// and those it named that nothing here is in, each by the name `--languages` takes.
type languagesJson struct {
	Kind     string   `json:"kind"`
	Named    []string `json:"named"`
	Checked  []string `json:"checked"`
	LeftOut  []string `json:"leftOut"`
	NotFound []string `json:"notFound"`
}

// languagesJsonOf is the record of what choice made of what discovery found.
func languagesJsonOf(choice languageChoice, found discovery) languagesJson {
	return languagesJson{
		Kind:  "languages",
		Named: slices.Clone(choice),
		Checked: languageNames(func(known language) bool {
			return choice.names(known.name) && !slices.Contains(found.NotFound, known.name)
		}),
		LeftOut:  append([]string{}, found.LeftOutLanguages...),
		NotFound: append([]string{}, found.NotFound...),
	}
}

// leftOutSegment is the footer's mark for the languages `--languages` left out that are here, so a run that checked
// some languages never reads as a green over every one: `⚠ left out by --languages: swift, markdown`. Empty when it
// left out none.
func leftOutSegment(leftOut []string) string {
	if len(leftOut) == 0 {
		return ""
	}
	return "⚠ left out by --languages: " + strings.Join(leftOut, ", ")
}
