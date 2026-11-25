package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/types/source_name"
)

// language is one name `--languages` or `--formats` takes (#s7gt4bj): the flag that takes it, the engine whose
// projects it checks, if any, and the files of its kind the house formatter formats, wherever they live, by
// extension. A language is code, checked, linted and formatted; a format is a kind of file cohere only formats. Every
// file a run checks is one language's or one format's, so a run naming some never checks the rest in silence.
type language struct {
	flag       string
	name       string
	engine     projectEngine
	extensions []string
}

// languagesFlagName and formatsFlagName are the two flags, by the names the command line spells.
const (
	languagesFlagName = "languages"
	formatsFlagName   = "formats"
)

// languages are the names the two flags take, in the order a list of them is said: the languages, then the formats.
// Read without regard to case, so `Go` and `go` name one. `typescript` takes JavaScript too, the programs and the
// loose files alike, and Adamic's `.a`, which a TypeScript program type-checks and lints as its own, until Adamic has
// a checker of its own (Kirk's design on #s7gt4bj). Every extension the house formatter registers is one name's
// (TestEveryFormattedExtensionIsOneLanguages).
var languages = []language{
	{flag: languagesFlagName, name: "go", engine: engineGo},
	{
		flag:   languagesFlagName,
		name:   "typescript",
		engine: engineTypeScript,
		extensions: []string{
			".ts", ".tsx", ".mts", ".cts", source_name.AdamicExtension, ".js", ".mjs", ".cjs", ".jsx",
		},
	},
	{flag: languagesFlagName, name: "swift", engine: engineSwift},
	{flag: formatsFlagName, name: "json", extensions: []string{".json"}},
	{flag: formatsFlagName, name: "markdown", extensions: []string{".md"}},
	{flag: formatsFlagName, name: "yaml", extensions: []string{".yaml", ".yml"}},
	{flag: formatsFlagName, name: "css", extensions: []string{".css"}},
	{flag: formatsFlagName, name: "graphql", extensions: []string{".graphql", ".gql"}},
}

// The two flags, read by main through languageChoiceOf. Here rather than beside main's other flags, since they are
// read nowhere else.
var (
	languagesFlag = flag.String(languagesFlagName, "",
		"check only these languages, a comma list of go, typescript (JavaScript and Adamic included) and swift; naming this or --formats checks exactly what the two name, and names the rest as left out")
	formatsFlag = flag.String(formatsFlagName, "",
		"format only these kinds of file, a comma list of json, markdown, yaml, css and graphql, wherever they live; naming this or --languages checks exactly what the two name, and names the rest as left out")
)

// languageChoice is the names the two flags named together, in languages' order, nil for a run that named neither,
// which checks everything it finds. Naming either narrows the run to exactly what the two name: `--languages go`
// alone checks no format, and `--formats json` alone no language.
type languageChoice []string

// languageChoiceOf is the choice the two flags made, from their values, nil when the command line named neither. Read
// before anything else is, so a misspelled name is refused wherever the run stands.
func languageChoiceOf(given map[string]bool, languagesValue string, formatsValue string) (languageChoice, error) {
	if !given[languagesFlagName] && !given[formatsFlagName] {
		return nil, nil
	}
	named := map[string]bool{}
	for _, flagName := range []string{languagesFlagName, formatsFlagName} {
		if !given[flagName] {
			continue
		}
		value := languagesValue
		if flagName == formatsFlagName {
			value = formatsValue
		}
		names, parseError := parseFlagNames(flagName, value)
		if parseError != nil {
			return nil, parseError
		}
		for _, name := range names {
			named[name] = true
		}
	}
	return languageNames(func(known language) bool { return named[known.name] }), nil
}

// parseFlagNames reads one flag's comma list against the names it takes. An unknown name, an empty list or an empty
// entry is refused with the names it takes, since a run that read a misspelled name as none would check less than
// was asked and say nothing.
func parseFlagNames(flagName string, value string) ([]string, error) {
	var names []string
	for entry := range strings.SplitSeq(value, ",") {
		name := strings.ToLower(strings.TrimSpace(entry))
		if !slices.ContainsFunc(languages, func(known language) bool {
			return known.flag == flagName && known.name == name
		}) {
			said := "an empty name"
			if name != "" {
				said = name
			}
			return nil, fmt.Errorf("--%s %s names %s, which it does not take: it takes %s, as a comma list, so "+
				"nothing was checked", flagName, value, said, sayList(flagNames(flagName)))
		}
		names = append(names, name)
	}
	return names, nil
}

// flagNames is the names one flag takes, in languages' order.
func flagNames(flagName string) []string {
	return languageNames(func(known language) bool { return known.flag == flagName })
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

// chooseNamedRunLanguages is what a choice does to a run that names its project rather than discovering: it checks
// the project only when the project's language is named, and its format walk leaves out every language and format
// not named, as a discovered project's run does through its yield. A run that discovers chooses from what it finds
// instead (chooseLanguages).
//
// A file named on its own, a path or the editor's save, is refused when its language or format is not named, since
// it would be formatted though it was left out.
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
	return refuseNamedFilesLeftOut(choice, append(slices.Clone(flag.Args()), flagValue("stdin-filepath")))
}

// refuseNamedFilesLeftOut refuses the first of namedFiles, the run's paths and the editor's save, whose language or
// format choice did not name, with a sentence naming it. A directory, or a file of no kind a run formats, is none,
// and passes.
func refuseNamedFilesLeftOut(choice languageChoice, namedFiles []string) error {
	leftOut := choice.leftOutExtensions()
	for _, named := range namedFiles {
		if named != "" && leavesOutFile(leftOut, named) {
			kind := kindOfFile(named)
			return fmt.Errorf("this run names %s, and not %s, the %s of %s, so nothing was checked", sayList(choice),
				kind.name, kindWord(kind), named)
		}
	}
	return nil
}

// kindOfFile is the language or format a file is, by its extension, read without regard to case: the zero language
// for a file of no kind a run formats.
func kindOfFile(fileName string) language {
	extension := strings.ToLower(filepath.Ext(fileName))
	for _, known := range languages {
		if slices.Contains(known.extensions, extension) {
			return known
		}
	}
	return language{}
}

// kindWord is what a name is called in a sentence: a language, or a format.
func kindWord(kind language) string {
	if kind.flag == formatsFlagName {
		return "format"
	}
	return "language"
}

// refuseUnchosenLanguage refuses a run that discovery does not apply to, one given `--directory` into one project, a
// tsconfig or paths, when its project's own language is not named, since a project run there checks that language
// first. A run that discovers, from the root or a directory holding several, checks what was named wherever it lives,
// a TypeScript program's JSON included (chooseLanguages). Nil when it is named, or when neither flag was.
func refuseUnchosenLanguage(choice languageChoice, location projectLocation, locateError error) error {
	if locateError != nil || choice.namesEngine(location.Engine) {
		return nil
	}
	// The format project is no language's own: it formats each name's files, which its walk keeps by name.
	if location.Engine == engineFormat && choice.formats() {
		return nil
	}
	return fmt.Errorf("this run names %s, and not %s, the language of the project at %s, so nothing was checked: run "+
		"from the root, which checks what was named wherever it lives", sayList(choice),
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

// choiceJson is what one of the two flags chose, as a record of the run's own under `--json`, written once before
// the projects' lines (#s7gt4bj): `languages` for the languages and `formats` for the formats, each with the names it
// named, those of them here that the run checked, those here it left out, and those it named that nothing here is
// in. A flag the command line did not name names none, and everything here of its kind is left out.
type choiceJson struct {
	Kind     string   `json:"kind"`
	Named    []string `json:"named"`
	Checked  []string `json:"checked"`
	LeftOut  []string `json:"leftOut"`
	NotFound []string `json:"notFound"`
}

// choiceJsonsOf are the two records of what choice made of what discovery found, the languages' then the formats'.
func choiceJsonsOf(choice languageChoice, found discovery) []choiceJson {
	var records []choiceJson
	for _, flagName := range []string{languagesFlagName, formatsFlagName} {
		ofFlag := func(names []string) []string {
			kept := []string{}
			for _, name := range names {
				if slices.ContainsFunc(languages, func(known language) bool {
					return known.flag == flagName && known.name == name
				}) {
					kept = append(kept, name)
				}
			}
			return kept
		}
		checked := languageNames(func(known language) bool {
			return choice.names(known.name) && !slices.Contains(found.NotFound, known.name)
		})
		records = append(records, choiceJson{
			Kind:     flagName,
			Named:    ofFlag(choice),
			Checked:  ofFlag(checked),
			LeftOut:  ofFlag(found.LeftOutLanguages),
			NotFound: ofFlag(found.NotFound),
		})
	}
	return records
}

// leftOutSegment is the footer's mark for the languages and formats here that the run did not name, so a run that
// checked some never reads as a green over everything: `⚠ left out: swift, markdown`. Empty when it left out none.
func leftOutSegment(leftOut []string) string {
	if len(leftOut) == 0 {
		return ""
	}
	return "⚠ left out: " + strings.Join(leftOut, ", ")
}
