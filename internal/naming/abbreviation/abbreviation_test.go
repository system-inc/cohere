package abbreviation_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// Every finding carries exactly the values its message's text uses, since each engine renders the id it names with
// those values and a missing one would print its placeholder. Run over one generated name per form of every entry,
// the same names nexus's tests judge, so no form of any word goes unchecked.
func TestFindGivesEachMessageItsValues(test *testing.T) {
	test.Parallel()
	valuesByMessage := map[string][]string{
		abbreviation.AbbreviatedIdentifier:  {"advice", "name"},
		abbreviation.AbbreviatedSuffix:      {"advice", "name", "suffix"},
		abbreviation.MillisecondSuffix:      {"name", "suggestion"},
		abbreviation.AbbreviatedWordSegment: {"advice", "name", "word"},
	}
	vocabulary := abbreviation.Policy
	for _, name := range oneNamePerForm(vocabulary) {
		finding, found := vocabulary.Find(name)
		if !found {
			test.Errorf("%q: not found", name)
			continue
		}
		want, known := valuesByMessage[finding.MessageId]
		if !known {
			test.Errorf("%q: message %q is none of the four", name, finding.MessageId)
			continue
		}
		if got := slices.Sorted(maps.Keys(finding.Values)); !slices.Equal(got, want) {
			test.Errorf("%q: values %v, want %v", name, got, want)
		}
	}
}

// A finding's spellings are the advice's own list: every one is quoted in the text the finding renders, so an engine
// that renames by them renames to what the message says, never to a second reading of the entry.
func TestFindSpellsOnlyWhatItsAdviceSays(test *testing.T) {
	test.Parallel()
	vocabulary := abbreviation.Policy
	for _, name := range oneNamePerForm(vocabulary) {
		finding, found := vocabulary.Find(name)
		if !found {
			test.Errorf("%q: not found", name)
			continue
		}
		// The millisecond message quotes its one suggestion itself, so it is quoted here the way the text reads.
		text := finding.Values["advice"] + `"` + finding.Values["suggestion"] + `"`
		for _, spelling := range finding.Spellings {
			if !strings.Contains(text, `"`+spelling+`"`) {
				test.Errorf("%q: the spelling %q is not in the advice %q", name, spelling, text)
			}
		}
	}
}

// The spellings of each form, in the name's own case, one per meaning, and none where the advice names no name.
func TestFindNamesTheSpellingsItsAdviceOffers(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		want []string
	}{
		{"cfg", []string{"configuration"}},
		// The vocabulary names Go's own word for it, and leaves to an engine whether that word is free.
		{"err", []string{"error"}},
		{"cfgPath", []string{"configurationPath"}},
		{"parseCfgFile", []string{"parseConfigurationFile"}},
		{"MaxLen", []string{"MaxLength"}},
		{"ownerRef", []string{"ownerReference"}},
		{"timeoutMs", []string{"timeoutInMilliseconds"}},
		{"pos", []string{"position", "positive"}},
		{"resultPos", []string{"resultPosition", "resultPositive"}},
		{"fetchRes", []string{"fetchResult", "fetchResponse", "fetchResource", "fetchReserve"}},
		{"ch", []string{"channel", "character"}},
		{"outputDirPath", []string{"outputDirectoryPath", "outputDirectionPath"}},
		// `Var` can mean JavaScript's var keyword, which no word spells out, so its advice names no name.
		{"themeVarName", nil},
		{"themeVars", nil},
		{"sb", nil},
		{"sbBottom", nil},
		{"scrollSbOffset", nil},
		{"fn", nil},
		{"argCount", nil},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			finding, found := abbreviation.Policy.Find(testCase.name)
			if !found {
				test.Fatalf("%q: not found", testCase.name)
			}
			if !slices.Equal(finding.Spellings, testCase.want) {
				test.Errorf("spellings %q, want %q", finding.Spellings, testCase.want)
			}
		})
	}
}

// oneNamePerForm is one generated name per form of every entry, the same names nexus's tests judge.
func oneNamePerForm(vocabulary *abbreviation.Vocabulary) []string {
	names := slices.Collect(maps.Keys(vocabulary.WholeByName))
	for _, entry := range slices.Concat(vocabulary.EarlyPrefixes, vocabulary.LatePrefixes) {
		names = append(names, entry.Abbreviation+"Widget")
	}
	for _, entry := range vocabulary.Suffixes {
		names = append(names, "widget"+abbreviation.Capitalize(entry.Abbreviation))
	}
	for _, entry := range vocabulary.Segments {
		names = append(names, "widget"+abbreviation.Capitalize(entry.Abbreviation)+"Name")
	}
	return names
}

// A name holding none of the words is no finding, and an allowed name is none even though it holds one.
func TestFindStaysQuietOnFullWords(test *testing.T) {
	test.Parallel()
	for _, name := range []string{"requestContext", "readError", "position", "URLSearchParams", "InnoDbIndex"} {
		if finding, found := abbreviation.Policy.Find(name); found {
			test.Errorf("%q reads as an abbreviation: %+v", name, finding)
		}
	}
}

// reasoning is the sentence every message but the millisecond one ends on, the `reasoning` phrase of the rule's
// message file, written out here so a rendered text is compared whole.
const reasoning = "A name is written once and read everywhere, so the letters saved at the declaration are paid back " +
	"at every call site by a reader who has to expand the abbreviation themselves and hope they expanded it the way " +
	"the author meant."

// rendered is a finding as the TypeScript rule prints it, from the rule's entry in the policy message catalog.
func rendered(test *testing.T, vocabulary *abbreviation.Vocabulary, name string) string {
	test.Helper()
	finding, found := vocabulary.Find(name)
	if !found {
		test.Fatalf("%q: not found", name)
	}
	return policy.MessageOf("nexus/consistency-no-abbreviated-identifier", finding.MessageId).Render(finding.Values)
}

// A word with one expansion renders every form byte for byte as it did when an entry held a single expansion, so
// the move to a list changed no finding anyone has read. The texts are the ones the rule printed before the move.
func TestFindRendersAWordWithOneExpansionAsBefore(test *testing.T) {
	test.Parallel()
	cases := []struct{ name, want string }{
		{"idx", `Identifier "idx" should not be abbreviated. Use "index" or a more descriptive name.`},
		{"db", `Identifier "db" should not be abbreviated. Use "database".`},
		{"idxStart", `Identifier "idxStart" should not be abbreviated. Use "indexStart".`},
		{"rowProps", `Identifier "rowProps" should not end with "Props". Use "rowProperties".`},
		{"buildEnvName", `Identifier "buildEnvName" abbreviates "Env". Use "buildEnvironmentName".`},
		{"args", `Identifier "args" should not be abbreviated. Use "arguments", "commandArguments", ` +
			`"commandLineArguments", or a more descriptive name.`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := rendered(test, abbreviation.Policy, testCase.name); got != testCase.want+" "+reasoning {
				test.Errorf("got %q, want %q", got, testCase.want+" "+reasoning)
			}
		})
	}
}

// `pos` means position or positive, and a census found `resultPos` beside `resultNeg`, where it is positive, read
// back as `resultPosition`. Every form names both meanings now, in the file's order, and asks for whichever it is.
// `res`, `ch` and `dir` are the same: an audit of the Go fix's renames found each guessed, `res` read as a response
// where it meant a result or MakeMap's reserve, `ch` as a channel where it was a rune, `dir` as a directory where it
// was a ChanDir.
func TestFindNamesEveryMeaningOfAWordWithMore(test *testing.T) {
	test.Parallel()
	listed, loadError := abbreviation.Load([]byte(`{"abbreviations": [
		{"abbreviation": "neg", "expansions": ["negative", "negation"], "whole": {"style": "plain"}, "suffix": {}},
		{"abbreviation": "rel", "expansions": ["relative", "relation", "release"], "segment": {}}
	]}`), nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	cases := []struct {
		name       string
		vocabulary *abbreviation.Vocabulary
		want       string
	}{
		{"pos", abbreviation.Policy, `Identifier "pos" should not be abbreviated. ` +
			`Use "position" or "positive", whichever it means, or a more descriptive name.`},
		{"posLabel", abbreviation.Policy, `Identifier "posLabel" should not be abbreviated. ` +
			`Use "positionLabel" or "positiveLabel", whichever it means.`},
		{"resultPos", abbreviation.Policy, `Identifier "resultPos" abbreviates "Pos". ` +
			`Use "resultPosition" or "resultPositive", whichever it means.`},
		{"res", abbreviation.Policy, `Identifier "res" should not be abbreviated. ` +
			`Use "result", "response", "resource" or "reserve", whichever it means, or a more descriptive name.`},
		{"resBody", abbreviation.Policy, `Identifier "resBody" should not be abbreviated. ` +
			`Use "resultBody", "responseBody", "resourceBody" or "reserveBody", whichever it means.`},
		{"fetchRes", abbreviation.Policy, `Identifier "fetchRes" should not end with "Res". ` +
			`Use "fetchResult", "fetchResponse", "fetchResource" or "fetchReserve", whichever it means.`},
		{"ch", abbreviation.Policy, `Identifier "ch" should not be abbreviated. ` +
			`Use "channel" or "character", whichever it means, or a more descriptive name.`},
		{"doneCh", abbreviation.Policy, `Identifier "doneCh" abbreviates "Ch". ` +
			`Use "doneChannel" or "doneCharacter", whichever it means.`},
		{"outputDirPath", abbreviation.Policy, `Identifier "outputDirPath" abbreviates "Dir". ` +
			`Use "outputDirectoryPath" or "outputDirectionPath", whichever it means.`},
		// The plain whole form, which no word in policy's file has two meanings in yet.
		{"neg", listed, `Identifier "neg" should not be abbreviated. ` +
			`Use "negative" or "negation", whichever it means.`},
		{"resultNeg", listed, `Identifier "resultNeg" should not end with "Neg". ` +
			`Use "resultNegative" or "resultNegation", whichever it means.`},
		// Three meanings are a list: commas, then "or" before the last.
		{"readRelPath", listed, `Identifier "readRelPath" abbreviates "Rel". ` +
			`Use "readRelativePath", "readRelationPath" or "readReleasePath", whichever it means.`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := rendered(test, testCase.vocabulary, testCase.name); got != testCase.want+" "+reasoning {
				test.Errorf("got %q, want %q", got, testCase.want+" "+reasoning)
			}
		})
	}
}

// `Var` and `Vars` can mean JavaScript's var keyword, as a rule against it is named `NoVar`, and the keyword has no
// full word to expand to. So they offer no spelling: they say what to write for a variable, and ask for a name saying
// it means the keyword otherwise. `vars` still precedes `var`, so a name holding both reports the longer word.
func TestFindAsksWhetherAWordMeansTheKeyword(test *testing.T) {
	test.Parallel()
	cases := []struct{ name, want string }{
		{"NoVar", `Identifier "NoVar" abbreviates "Var". ` +
			`Use "Variable" for a variable, or a name saying it means JavaScript's var keyword.`},
		{"themeVarName", `Identifier "themeVarName" abbreviates "Var". ` +
			`Use "Variable" for a variable, or a name saying it means JavaScript's var keyword.`},
		{"themeVars", `Identifier "themeVars" abbreviates "Vars". ` +
			`Use "Variables" for variables, or a name saying it means JavaScript's var keyword.`},
		{"themeVarsVar", `Identifier "themeVarsVar" abbreviates "Vars". ` +
			`Use "Variables" for variables, or a name saying it means JavaScript's var keyword.`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if got := rendered(test, abbreviation.Policy, testCase.name); got != testCase.want+" "+reasoning {
				test.Errorf("got %q, want %q", got, testCase.want+" "+reasoning)
			}
		})
	}
}

// `sb` is Go's habit for a strings.Builder, but a census found it naming a scroll region's bottom, and its letters
// fit more words than a list could hold. So no form suggests a word: each asks what it holds.
func TestFindAsksWhatAnUnlistableWordHolds(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		form abbreviation.Form
		want string
	}{
		{"sb", abbreviation.FormWhole, `Identifier "sb" should not be abbreviated. Spell out what it holds.`},
		// An advice-style prefix names the abbreviation rather than the name, as the role words always have.
		{"sbBottom", abbreviation.FormPrefix, `Identifier "sb" should not be abbreviated. Spell out what it holds.`},
		{"scrollSbOffset", abbreviation.FormSegment, `Identifier "scrollSbOffset" abbreviates "Sb". ` +
			`Spell out what it holds.`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if finding, _ := abbreviation.Policy.Find(testCase.name); finding.Form != testCase.form {
				test.Errorf("form %q, want %q", finding.Form, testCase.form)
			}
			got := rendered(test, abbreviation.Policy, testCase.name)
			if got != testCase.want+" "+reasoning {
				test.Errorf("got %q, want %q", got, testCase.want+" "+reasoning)
			}
			if strings.Contains(got, "stringBuilder") {
				test.Errorf("%q still guesses a builder", got)
			}
		})
	}
}

// The loader refuses an expansion list the engines would read as something other than meant, and the key it
// replaced, so a file written for the single expansion fails at startup rather than judging with none.
func TestLoadRefusesABrokenExpansionList(test *testing.T) {
	test.Parallel()
	cases := []struct{ name, file string }{
		{"an empty list where a form renames", `{"abbreviations": [
			{"abbreviation": "val", "expansions": [], "whole": {"style": "plain"}}]}`},
		{"an empty word in the list", `{"abbreviations": [
			{"abbreviation": "val", "expansions": ["value", ""], "segment": {}}]}`},
		{"the retired single expansion", `{"abbreviations": [
			{"abbreviation": "val", "expansion": "value", "whole": {"style": "plain"}}]}`},
		{"a lone word rather than a list", `{"abbreviations": [
			{"abbreviation": "val", "expansions": "value", "whole": {"style": "plain"}}]}`},
		{"an unknown segment style", `{"abbreviations": [
			{"abbreviation": "val", "expansions": ["value"], "segment": {"style": "loud"}}]}`},
		{"a segment asking for advice the entry lacks", `{"abbreviations": [
			{"abbreviation": "val", "expansions": ["value"], "segment": {"style": "advice"}}]}`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if _, loadError := abbreviation.Load([]byte(testCase.file), nil); loadError == nil {
				test.Errorf("the loader accepted it")
			}
		})
	}
	// The clean case beside them: advice alone carries a word with no expansions through every form that takes it.
	if _, loadError := abbreviation.Load([]byte(`{"abbreviations": [{"abbreviation": "sb", "advice": "Say.",
		"whole": {"style": "advice"}, "prefix": {"phase": "late", "style": "advice"},
		"segment": {"style": "advice"}}]}`,
	), nil); loadError != nil {
		test.Errorf("an advice-only entry is refused: %v", loadError)
	}
}

// Find tests the prefix, suffix and segment forms as literal text, which says what the patterns say only while an
// abbreviation is letters a to z. So the loader refuses anything else, rather than let a digit, a pattern's syntax
// or a letter that capitalizes in more than one byte quietly change what a form matches.
func TestLoadRefusesAnAbbreviationThatIsNotLetters(test *testing.T) {
	test.Parallel()
	for _, abbreviationText := range []string{"db2", "d.b", "d+", "Db", "dé", "d_b", ""} {
		test.Run(abbreviationText, func(test *testing.T) {
			test.Parallel()
			file := `{"abbreviations": [{"abbreviation": "` + abbreviationText + `", "expansions": ["database"], ` +
				`"prefix": {"phase": "late"}}]}`
			if _, loadError := abbreviation.Load([]byte(file), nil); loadError == nil {
				test.Errorf("the loader accepted %q", abbreviationText)
			}
		})
	}
}
