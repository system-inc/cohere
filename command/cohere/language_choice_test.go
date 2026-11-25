package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/native"
)

// `--languages` and `--formats` each read a comma list without regard to case, and together make one choice, in the
// table's order; each refuses a name it does not take with every name it does, since a misspelled name read as none
// would check less than was asked (#s7gt4bj). Adamic's files are TypeScript's, so `adamic` is no name either takes.
func TestTheTwoFlagsMakeOneChoiceAndRefuseWhatTheyDoNotTake(test *testing.T) {
	test.Parallel()
	for _, read := range []struct {
		languages, formats string
		want               languageChoice
	}{
		{languages: "go", want: languageChoice{"go"}},
		{formats: "json", want: languageChoice{"json"}},
		{
			languages: "TypeScript,Go",
			formats:   " Markdown , json ",
			want:      languageChoice{"go", "typescript", "json", "markdown"},
		},
		{languages: "typescript,go,typescript", want: languageChoice{"go", "typescript"}},
	} {
		given := map[string]bool{languagesFlagName: read.languages != "", formatsFlagName: read.formats != ""}
		choice, refusal := languageChoiceOf(given, read.languages, read.formats)
		if refusal != nil || !slices.Equal(choice, read.want) {
			test.Errorf("--languages %q --formats %q read %v (refusal %v), want %v", read.languages, read.formats,
				choice, refusal, read.want)
		}
	}
	if choice, refusal := languageChoiceOf(map[string]bool{}, "", ""); choice != nil || refusal != nil {
		test.Errorf("a run naming neither flag chose %v (refusal %v), want nothing", choice, refusal)
	}
	for _, refused := range []struct{ flagName, value, said, takes string }{
		{languagesFlagName, "json", "names json,", "it takes go, typescript and swift"},
		{languagesFlagName, "adamic", "names adamic,", "it takes go, typescript and swift"},
		{languagesFlagName, "go,", "names an empty name,", "it takes go, typescript and swift"},
		{formatsFlagName, "go", "names go,", "it takes json, markdown, yaml, css and graphql"},
		{formatsFlagName, "", "names an empty name,", "it takes json, markdown, yaml, css and graphql"},
	} {
		values := map[string]string{refused.flagName: refused.value}
		_, refusal := languageChoiceOf(map[string]bool{refused.flagName: true}, values[languagesFlagName],
			values[formatsFlagName])
		if refusal == nil || !strings.Contains(refusal.Error(), "--"+refused.flagName+" "+refused.value+" "+
			refused.said) || !strings.Contains(refusal.Error(), refused.takes) {
			test.Errorf("--%s %q was refused with %v, want one saying it %s and %s", refused.flagName, refused.value,
				refusal, refused.said, refused.takes)
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
				{Directory: ".", Kind: projectKindGoModule},
				{Directory: "web", Kind: projectKindTypeScriptProgram},
				{Directory: "web", Kind: projectKindTypeScriptProgram, ConfigFile: "tsconfig.app.json"},
				{Directory: "swift", Kind: projectKindSwiftPackage},
				{Directory: ".", Kind: projectKindFolder},
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
			said = append(said, project.Directory+" "+project.Kind.label())
		}
		return said
	}

	everything := discovered()
	everything.chooseLanguages(nil, true)
	if len(everything.Projects) != 5 || everything.LeftOut != nil || everything.LeftOutLanguages != nil {
		test.Errorf("no choice runs %v and leaves out %v", running(everything), everything.LeftOutLanguages)
	}

	goAlone := discovered()
	goAlone.chooseLanguages(languageChoice{"go"}, true)
	if !slices.Equal(running(goAlone), []string{". Go"}) ||
		!slices.Equal(goAlone.LeftOutLanguages, []string{"typescript", "swift", "json", "markdown"}) {
		test.Errorf("go runs %v and leaves out %v", running(goAlone), goAlone.LeftOutLanguages)
	}

	// The programs' directory gets one format project, its walk keeping JSON alone, and the root's walk leaves the
	// programs' directory to it.
	jsonAlone := discovered()
	jsonAlone.chooseLanguages(languageChoice{"json"}, true)
	if !slices.Equal(running(jsonAlone), []string{". Folder", "web Folder"}) {
		test.Errorf(
			"json runs %v, want the root's format project and one of the programs' directory",
			running(jsonAlone),
		)
	}
	if yield := jsonAlone.formatYield(discoveredProject{Directory: ".", Kind: projectKindFolder}); !slices.Equal(
		yield.Directories,
		[]string{"/root/web"},
	) {
		test.Errorf("the root's format project leaves %v to others, want the programs' directory", yield.Directories)
	}
	if yield := jsonAlone.formatYield(
		discoveredProject{Directory: "web", Kind: projectKindFolder},
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
	typeScript.chooseLanguages(languageChoice{"typescript", "css"}, true)
	if !slices.Equal(running(typeScript), []string{"web TypeScript", "web TypeScript"}) ||
		!slices.Equal(typeScript.NotFound, []string{"css"}) {
		test.Errorf("typescript and css run %v and find no %v", running(typeScript), typeScript.NotFound)
	}
}

// A plain run's format project lists a directory twice when two programs share it, as it always did, so its yield,
// and the replay key its fingerprint is in, do not move (#s7gt4bj); a run under `--languages` lists it once. And a
// run that does not format turns no program into a format project, so a language only a format walk would check
// runs nowhere.
func TestAPlainRunsYieldIsAsItWasAndARunThatDoesNotFormatConvertsNothing(test *testing.T) {
	test.Parallel()
	discovered := func() discovery {
		return discovery{
			Root: "/root",
			Projects: []discoveredProject{
				{Directory: "web", Kind: projectKindTypeScriptProgram},
				{Directory: "web", Kind: projectKindTypeScriptProgram, ConfigFile: "tsconfig.app.json"},
			},
			FormattableKinds: map[string]map[string]int{"web": {".json": 1}},
		}
	}
	root := discoveredProject{Directory: ".", Kind: projectKindFolder}
	whole := discovered()
	whole.chooseLanguages(nil, true)
	if yield := whole.formatYield(root); !slices.Equal(yield.Directories, []string{"/root/web", "/root/web"}) {
		test.Errorf("a plain run's format project yields %v, want the shared directory twice, as before",
			yield.Directories)
	}
	notFormatting := discovered()
	notFormatting.chooseLanguages(languageChoice{"json"}, false)
	if len(notFormatting.Projects) != 0 {
		test.Errorf("json in a run that does not format runs %v, want nothing", notFormatting.Projects)
	}
}

// A file named on its own, a path or the editor's save, in a language not named is refused with its language, read
// without regard to case, since a run would format it though its language was left out (#s7gt4bj). A directory, a
// file in a named language, and a file of no language a run formats pass.
func TestANamedFileInALanguageNotNamedIsRefused(test *testing.T) {
	test.Parallel()
	choice := languageChoice{"typescript"}
	for _, named := range []string{"README.md", "docs/NOTES.MD"} {
		refusal := refuseNamedFilesLeftOut(choice, []string{"source/Tidy.ts", named})
		if refusal == nil || !strings.Contains(refusal.Error(),
			"this run names typescript, and not markdown, the format of "+named) {
			test.Errorf("naming %s was refused with %v", named, refusal)
		}
	}
	if refusal := refuseNamedFilesLeftOut(choice, []string{"source", "source/Tidy.ts", "go.mod", ""}); refusal != nil {
		test.Errorf("a directory, a TypeScript file, a file of no language or no save was refused: %v", refusal)
	}
}

// The front door answers `--languages` for a Go or Swift run it hands a child, so neither language's run refuses it
// as a flag it does not take (#s7gt4bj).
func TestAGoOrSwiftRunTakesLanguages(test *testing.T) {
	test.Parallel()
	given := map[string]bool{languagesFlagName: true, formatsFlagName: true}
	value := func(string) string { return "go,swift" }
	location := projectLocation{Root: "/project", ArgumentBase: "/project", WorkingDirectory: "/project"}
	for name, flags := range map[string]contractFlags{"Go": goFlags, "Swift": swiftFlags} {
		if _, _, refusal := contractArguments(
			goLanguage("module"),
			flags,
			location,
			given,
			value,
			nil,
		); refusal != nil {
			test.Errorf("a %s run refused --languages: %v", name, refusal)
		}
	}
}

// The verbose account's summary line names what `--languages` left out (#s7gt4bj).
func TestTheSummaryLineNamesWhatLanguagesLeftOut(test *testing.T) {
	test.Parallel()
	found := discovery{LeftOutLanguages: []string{"swift", "markdown"}}
	runs := []projectRun{{project: discoveredProject{Directory: ".", Kind: projectKindGoModule}, label: "."}}
	if line := summaryLine(found, runs); !strings.Contains(line, "left out: swift, markdown") {
		test.Errorf("the summary line reads %q", line)
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
		{"--formats=json"},
		{"--formats", "markdown", "--no-fix"},
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
		{"--no-fix", "-languages", "typescript"},
		{"--no-fix", "--formats", "json", "--languages=go"},
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
	want := "⚠ left out: swift, markdown"
	facts := overallFacts{ProjectsByKind: map[string]int{"Go": 1}, LeftOut: []string{"swift", "markdown"}}
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
	location := projectLocation{Root: "/root/app", Kind: projectKindSwiftPackage}
	refusal := refuseUnchosenLanguage(languageChoice{"go", "typescript"}, location, nil)
	if refusal == nil || !strings.Contains(refusal.Error(),
		"this run names go and typescript, and not swift, the language of the project at /root/app") {
		test.Errorf("refusal %v", refusal)
	}
	if refuseUnchosenLanguage(languageChoice{"swift"}, location, nil) != nil ||
		refuseUnchosenLanguage(nil, location, nil) != nil ||
		refuseUnchosenLanguage(languageChoice{"json"}, projectLocation{Kind: projectKindFolder}, nil) != nil {
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
