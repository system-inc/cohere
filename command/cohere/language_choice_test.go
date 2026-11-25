package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/native"
)

// `--languages` reads a comma list without regard to case, once each, in its own order, and refuses a name it does
// not take with every name it does, since a misspelled language read as none would check less than was asked
// (#s7gt4bj).
func TestLanguagesAreReadAsACommaListAndAnUnknownOneIsRefused(test *testing.T) {
	test.Parallel()
	for value, want := range map[string]languageChoice{
		"go":                       {"go"},
		"TypeScript,Go":            {"go", "typescript"},
		" markdown , json ":        {"json", "markdown"},
		"typescript,go,typescript": {"go", "typescript"},
	} {
		choice, parseError := parseLanguageChoice(value)
		if parseError != nil || !slices.Equal(choice, want) {
			test.Errorf("--languages %q read %v (refusal %v), want %v", value, choice, parseError, want)
		}
	}
	every := "it takes go, typescript, swift, adamic, json, markdown, yaml, css and graphql"
	for value, named := range map[string]string{
		"rust":    "names rust,",
		"go,rust": "names rust,",
		"":        "names an empty name,",
		"go,":     "names an empty name,",
	} {
		_, parseError := parseLanguageChoice(value)
		if parseError == nil || !strings.Contains(parseError.Error(), named) ||
			!strings.Contains(parseError.Error(), every) {
			test.Errorf("--languages %q was refused with %v, want one saying it %s and naming every language", value,
				parseError, named)
		}
	}
}

// Every file the house formatter formats is in exactly one language, so a run naming some languages never formats
// the rest in silence, and a printer added later fails here until it has a name (#s7gt4bj).
func TestEveryFormattedExtensionIsOneLanguages(test *testing.T) {
	test.Parallel()
	for _, extension := range native.Extensions() {
		var owners []string
		for _, known := range languages {
			if slices.Contains(known.extensions, extension) {
				owners = append(owners, known.name)
			}
		}
		if len(owners) != 1 {
			test.Errorf("the formatter's %s is in the languages %v, want exactly one", extension, owners)
		}
	}
	for _, known := range languages {
		for _, extension := range known.extensions {
			if !slices.Contains(native.Extensions(), extension) {
				test.Errorf("%s names %s, which the formatter does not format", known.name, extension)
			}
		}
	}
}

// A run checks the languages it named wherever their files live (#s7gt4bj): a Go or Swift project when its language
// is named; a TypeScript program whole for `typescript`, and for a language it formats alone, as a format project of
// its directory; the root's format project for any language it formats. What is here and not named is left out by
// name, and what is named and not here is noted. A run that named none changes nothing.
func TestChosenLanguagesRunWhereverTheirFilesLive(test *testing.T) {
	test.Parallel()
	discovered := func() discovery {
		return discovery{
			Root: "/root",
			Projects: []discoveredProject{
				{Directory: ".", Engine: engineGo},
				{Directory: "web", Engine: engineTypeScript},
				{Directory: "web", Engine: engineTypeScript, ConfigFile: "tsconfig.app.json"},
				{Directory: "swift", Engine: engineSwift},
				{Directory: ".", Engine: engineFormat},
			},
			FormattableKinds: map[string]map[string]int{
				".":   {".json": 3, ".md": 1},
				"web": {".ts": 2, ".json": 1},
			},
		}
	}
	running := func(found discovery) []string {
		var said []string
		for _, project := range found.Projects {
			said = append(said, project.Directory+" "+string(project.Engine))
		}
		return said
	}

	everything := discovered()
	everything.chooseLanguages(nil)
	if len(everything.Projects) != 5 || everything.LeftOut != nil || everything.LeftOutLanguages != nil {
		test.Errorf("no choice runs %v and leaves out %v", running(everything), everything.LeftOutLanguages)
	}

	goAlone := discovered()
	goAlone.chooseLanguages(languageChoice{"go"})
	if !slices.Equal(running(goAlone), []string{". Go"}) ||
		!slices.Equal(goAlone.LeftOutLanguages, []string{"typescript", "swift", "json", "markdown"}) {
		test.Errorf("go runs %v and leaves out %v", running(goAlone), goAlone.LeftOutLanguages)
	}

	// The programs' directory gets one format project, its walk keeping JSON alone, and the root's walk leaves the
	// programs' directory to it.
	jsonAlone := discovered()
	jsonAlone.chooseLanguages(languageChoice{"json"})
	if !slices.Equal(running(jsonAlone), []string{". Format", "web Format"}) {
		test.Errorf(
			"json runs %v, want the root's format project and one of the programs' directory",
			running(jsonAlone),
		)
	}
	if yield := jsonAlone.formatYield(discoveredProject{Directory: ".", Engine: engineFormat}); !slices.Equal(
		yield.Directories,
		[]string{"/root/web"},
	) {
		test.Errorf("the root's format project leaves %v to others, want the programs' directory", yield.Directories)
	}
	if yield := jsonAlone.formatYield(
		discoveredProject{Directory: "web", Engine: engineFormat},
	); len(yield.Directories) != 0 {
		test.Errorf("the programs' format project leaves its own directory %v to others", yield.Directories)
	}
	if leftOut, want := jsonAlone.Choice.leftOutExtensions(), []string{
		".a", ".cjs", ".css", ".cts", ".gql", ".graphql", ".js", ".jsx", ".md", ".mjs", ".mts", ".ts", ".tsx", ".yaml",
		".yml",
	}; !slices.Equal(leftOut, want) {
		test.Errorf("json leaves out %v, want %v", leftOut, want)
	}

	// The root's format project keeps nothing, its JSON and Markdown not named and the TypeScript all the programs',
	// so it does not run: a run that checks nothing fails.
	typeScript := discovered()
	typeScript.chooseLanguages(languageChoice{"typescript", "css"})
	if !slices.Equal(running(typeScript), []string{"web TypeScript", "web TypeScript"}) ||
		!slices.Equal(typeScript.NotFound, []string{"css"}) {
		test.Errorf("typescript and css run %v and find no %v", running(typeScript), typeScript.NotFound)
	}
}

// A narrowed run never replays, nor records a run another could replay, since `--languages` is no flag the run cache
// admits in any spelling (#s7gt4bj); and a project's run is never handed it, its yield carrying what it leaves out.
func TestLanguagesNeverReachTheRunCacheOrAProjectsArguments(test *testing.T) {
	test.Parallel()
	for _, arguments := range [][]string{
		{"--languages=go"},
		{"--languages", "go"},
		{"--no-fix", "--languages=go,typescript"},
		{"-languages=go"},
	} {
		if runCacheAdmits(arguments) {
			test.Errorf("the run cache admits %q", arguments)
		}
	}
	if !runCacheAdmits([]string{"--no-fix"}) {
		test.Errorf("the run cache no longer admits a plain --no-fix, so this test proves nothing")
	}
	for _, arguments := range [][]string{
		{"--no-fix", "--languages", "go"},
		{"--languages=go,swift", "--no-fix"},
		{"--no-fix", "-languages", "json"},
	} {
		if got := childArguments(arguments, "", false); !slices.Equal(got, []string{"--no-fix"}) {
			test.Errorf("a project's run of %q is handed %q, want the plain run's --no-fix", arguments, got)
		}
	}
}

// What a project's run leaves out is in its yield's fingerprint, and so in its replay key, and a run that named no
// languages encodes its yield as it always did, so its key does not move (#s7gt4bj).
func TestTheLeftOutLanguagesMoveTheYieldsEncodingOnlyWhenNamed(test *testing.T) {
	test.Parallel()
	whole := projectYield{Directories: []string{"/root/web"}}
	encoded := mustEncode(test, whole)
	if encoded != `{"files":null,"directories":["/root/web"]}` {
		test.Errorf("a yield with nothing left out encodes as %s, which moves every plain run's key", encoded)
	}
	narrowed := whole
	narrowed.LeftOutExtensions = []string{".md"}
	if mustEncode(test, narrowed) == encoded {
		test.Errorf("a yield leaving out markdown encodes as the plain one, so a narrowed run could replay a whole one")
	}
}

// The footers name the languages `--languages` left out, so a narrowed green never reads as one over every language,
// and a run that left out none says nothing of it (#s7gt4bj).
func TestTheFootersNameWhatLanguagesLeftOut(test *testing.T) {
	test.Parallel()
	want := "⚠ left out by --languages: swift, markdown"
	facts := overallFacts{ProjectsByEngine: map[string]int{"Go": 1}, LeftOut: []string{"swift", "markdown"}}
	if line := overallFooter(facts, plain); !strings.Contains(line, want) || !strings.HasPrefix(line, "✓ 💎 ") {
		test.Errorf("the overall footer reads %q, want a green naming %q", line, want)
	}
	if line := combinedFooter(nil, facts, 0, plain); !strings.Contains(line, want) {
		test.Errorf("the combined footer reads %q, want it naming %q", line, want)
	}
	facts.LeftOut = nil
	if line := overallFooter(facts, plain); strings.Contains(line, "left out") {
		test.Errorf("a run that left out nothing reads %q", line)
	}
}

// A run that names its project, not discovering, is refused when that project's own language is not named, and
// pointed at the root, which checks a named language wherever it lives (#s7gt4bj).
func TestARunNamingAProjectWhoseLanguageIsNotNamedIsRefused(test *testing.T) {
	test.Parallel()
	location := projectLocation{Root: "/root/app", Engine: engineSwift}
	refusal := refuseUnchosenLanguage(languageChoice{"go", "typescript"}, location, nil)
	if refusal == nil || !strings.Contains(refusal.Error(),
		"--languages go and typescript does not name swift, the language of the project at /root/app") {
		test.Errorf("refusal %v", refusal)
	}
	if refuseUnchosenLanguage(languageChoice{"swift"}, location, nil) != nil ||
		refuseUnchosenLanguage(nil, location, nil) != nil ||
		refuseUnchosenLanguage(languageChoice{"json"}, projectLocation{Engine: engineFormat}, nil) != nil {
		test.Errorf("a project in a named language, a run that named none, or a format project of a named kind, was " +
			"refused")
	}
}

// mustEncode is a value as the yield file holds it.
func mustEncode(test *testing.T, value projectYield) string {
	test.Helper()
	encoded, encodeError := json.Marshal(value)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return string(encoded)
}
