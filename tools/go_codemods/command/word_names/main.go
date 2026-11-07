// Command wordnames expands the abbreviated words in the names of cohere's Go locals: `cfg` becomes
// `configuration`, `srcPath` `sourcePath`, `cmd` `command`. The words, and where in a name each is judged,
// come from policy/Abbreviations.json, read the way nexus/consistency-no-abbreviated-identifier reads it, so a
// renamed local is one that rule no longer flags. Every rename goes through the rename engine, which refuses
// any that would change what an identifier resolves to; after writing, the tree is reloaded and every
// identifier's resolution compared. A local it cannot settle by itself is listed as a wave, and an
// abbreviated name that is not a local is counted, never proposed. With -global it renames those names instead,
// and the locals holding an advice-only word, through the global rename engine (global.go); with
// -rename-owned-spellings as well, the names spelling an upstream rule or an outside name (external.go).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/policy"

	"golang.org/x/tools/go/types/typeutil"
)

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code wordnames exits with: runCommandLine returns its exit code here, and
// streams.Finish delivers what was printed and turns a clean exit into 1 when it did not arrive.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine reads the flags and runs wordnames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts, every rejection and every wave, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	vocabularyPath := flag.String(
		"vocabulary",
		"",
		"the abbreviation vocabulary (default: the one built into the pass, from the tree it is built in)",
	)
	global := flag.Bool(
		"global",
		false,
		"rename the flagged names that reach past one function, and the advice-word locals, through the global engine",
	)
	tagEncodedFields := flag.Bool(
		"tag-encoded-fields",
		false,
		"with -global, keep an encoded field's key by adding a tag that spells its old name",
	)
	limitLineLength := flag.Bool(
		"limit-line-length",
		false,
		"with -global, refuse a rename that would make a line newly longer than 120 columns",
	)
	renameOwnedSpellings := flag.Bool("rename-owned-spellings", false,
		"with -global, rename the names that spell an upstream rule or an outside name too, the outside name kept "+
			"in the doc comment")
	renameLibraryExports := flag.String("rename-library-exports", "",
		"with -global, the library packages outside internal/, comma-separated directories relative to the root, "+
			"whose exports may be renamed: every consumer outside the repository reads them at a pin and is told "+
			"when it moves")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	if *global {
		var libraries []string
		if *renameLibraryExports != "" {
			libraries = strings.Split(*renameLibraryExports, ",")
		}
		return runGlobal(*root, *vocabularyPath, skips, *dryRun, *renameOwnedSpellings, libraries,
			global_rename.Options{TagEncodedFields: *tagEncodedFields, LimitLineLength: *limitLineLength})
	}
	return run(*root, *vocabularyPath, skips, *dryRun)
}

// The vocabulary file's shape, mirroring the rule's abbreviationVocabularyFile. Unknown keys are refused, so
// a form this command does not know is a startup failure rather than a word it silently never expands.
type vocabularyFile struct {
	About           string            `json:"about"`
	Abbreviations   []vocabularyEntry `json:"abbreviations"`
	AllowedNames    []allowedName     `json:"allowedNames"`
	AllowedSegments []allowedSegment  `json:"allowedSegments"`
}

// wholeForm judges a name that is the abbreviation entire. Style is "orDescriptive", "plain" or "advice".
type wholeForm struct {
	Style string `json:"style"`
}

// prefixForm judges a name that begins with the abbreviation followed by a capital. Phase is "early" or
// "late"; Style is empty for a rename or "advice".
type prefixForm struct {
	Phase string `json:"phase"`
	Style string `json:"style"`
}

// suffixForm judges a name that ends with the abbreviation capitalized. Advice replaces the rename; Matcher
// "millisecondWord" replaces the plain suffix test, and Replacement the capitalized expansion.
type suffixForm struct {
	Advice      string `json:"advice"`
	Matcher     string `json:"matcher"`
	Replacement string `json:"replacement"`
}

// segmentForm judges the abbreviation capitalized as a camelCase word anywhere in a name. Style is empty for a
// rename or "advice".
type segmentForm struct {
	Style string `json:"style"`
}

type allowedName struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type allowedSegment struct {
	Segment string `json:"segment"`
	Reason  string `json:"reason"`
}

// segmentPatterns are the rule's three segment expressions for one word.
type segmentPatterns struct {
	boundary *regexp.Regexp
	camel    *regexp.Regexp
	replace  *regexp.Regexp
}

// vocabulary is the file turned into the tables each phase walks, in the file's order.
type vocabulary struct {
	wholeByName     map[string]*vocabularyEntry
	earlyPrefixes   []*vocabularyEntry
	latePrefixes    []*vocabularyEntry
	suffixes        []*vocabularyEntry
	segments        []*vocabularyEntry
	allowedNames    map[string]bool
	allowedSegments []string
	prefixPatterns  map[string]*regexp.Regexp
	segmentPatterns map[string]segmentPatterns
}

// millisecondPattern is the rule's `([a-z])Ms($|[A-Z])`: a millisecond unit as a camelCase word.
var millisecondPattern = regexp.MustCompile(`([a-z])Ms($|[A-Z])`)

// loadVocabulary reads the file as the rule does, refusing unknown keys and unknown form values.
func loadVocabulary(data []byte) (*vocabulary, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file vocabularyFile
	if decodeError := decoder.Decode(&file); decodeError != nil {
		return nil, decodeError
	}
	loaded := &vocabulary{
		wholeByName:     map[string]*vocabularyEntry{},
		allowedNames:    map[string]bool{},
		prefixPatterns:  map[string]*regexp.Regexp{},
		segmentPatterns: map[string]segmentPatterns{},
	}
	for index := range file.Abbreviations {
		entry := &file.Abbreviations[index]
		if entry.Abbreviation == "" || strings.ToLower(entry.Abbreviation) != entry.Abbreviation {
			return nil, fmt.Errorf("entry %d: the abbreviation must be lowercase and present", index)
		}
		if entry.Whole != nil {
			if entry.Whole.Style != "orDescriptive" && entry.Whole.Style != "plain" && entry.Whole.Style != "advice" {
				return nil, fmt.Errorf("entry %q: unknown whole style %q", entry.Abbreviation, entry.Whole.Style)
			}
			loaded.wholeByName[entry.Abbreviation] = entry
		}
		if entry.Prefix != nil {
			if entry.Prefix.Style != "" && entry.Prefix.Style != "advice" {
				return nil, fmt.Errorf("entry %q: unknown prefix style %q", entry.Abbreviation, entry.Prefix.Style)
			}
			prefixPattern, compileError := regexp.Compile(`^` + entry.Abbreviation + `[A-Z]`)
			if compileError != nil {
				return nil, fmt.Errorf("entry %q: compiling its prefix pattern: %w", entry.Abbreviation, compileError)
			}
			loaded.prefixPatterns[entry.Abbreviation] = prefixPattern
			switch entry.Prefix.Phase {
			case "early":
				loaded.earlyPrefixes = append(loaded.earlyPrefixes, entry)
			case "late":
				loaded.latePrefixes = append(loaded.latePrefixes, entry)
			default:
				return nil, fmt.Errorf("entry %q: unknown prefix phase %q", entry.Abbreviation, entry.Prefix.Phase)
			}
		}
		if entry.Suffix != nil {
			if entry.Suffix.Matcher != "" && entry.Suffix.Matcher != "millisecondWord" {
				return nil, fmt.Errorf("entry %q: unknown suffix matcher %q", entry.Abbreviation, entry.Suffix.Matcher)
			}
			loaded.suffixes = append(loaded.suffixes, entry)
		}
		if entry.Segment != nil {
			patterns, compileError := compileSegmentPatterns(capitalize(entry.Abbreviation))
			if compileError != nil {
				return nil, fmt.Errorf("entry %q: compiling its segment patterns: %w", entry.Abbreviation, compileError)
			}
			loaded.segments = append(loaded.segments, entry)
			loaded.segmentPatterns[entry.Abbreviation] = patterns
		}
	}
	for _, allowed := range file.AllowedNames {
		loaded.allowedNames[allowed.Name] = true
	}
	for _, allowed := range file.AllowedSegments {
		loaded.allowedSegments = append(loaded.allowedSegments, allowed.Segment)
	}
	return loaded, nil
}

// compileSegmentPatterns compiles the three patterns a segment form of word is found and replaced by. The word
// comes from the vocabulary file, so a word that is no valid pattern is an error in the file, not a panic.
func compileSegmentPatterns(word string) (segmentPatterns, error) {
	boundary, boundaryError := regexp.Compile(`(^|[^a-zA-Z])` + word + `($|[A-Z0-9])`)
	if boundaryError != nil {
		return segmentPatterns{}, boundaryError
	}
	camel, camelError := regexp.Compile(`[a-z0-9]` + word + `($|[A-Z0-9])`)
	if camelError != nil {
		return segmentPatterns{}, camelError
	}
	replace, replaceError := regexp.Compile(word + `($|[A-Z0-9])`)
	if replaceError != nil {
		return segmentPatterns{}, replaceError
	}
	return segmentPatterns{boundary: boundary, camel: camel, replace: replace}, nil
}

// capitalize is the spelling a suffix or segment form matches: `prop` becomes `Prop`.
func capitalize(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}

// replaceFirst replaces only the first match, as the rule's suggestion does.
func replaceFirst(pattern *regexp.Regexp, text string, replacement string) string {
	location := pattern.FindStringSubmatchIndex(text)
	if location == nil {
		return text
	}
	expanded := pattern.ExpandString(nil, replacement, text, location)
	return text[:location[0]] + string(expanded) + text[location[1]:]
}

// finding is what the vocabulary says about a name: the form and word that matched first, and the name
// with that one word expanded, which is empty when the form gives advice instead of a rename.
type finding struct {
	form       string
	entry      *vocabularyEntry
	suggestion string
}

// find is the rule's vocabulary.find, phase for phase: whole word, early prefix, suffix, late prefix,
// segment, the first match reporting.
func (vocabulary *vocabulary) find(name string) (finding, bool) {
	return vocabulary.findMeaning(name, nil)
}

// findMeaning is find with the expansion of each word in meanings replaced by the meaning given there, so a
// local whose type says what an abbreviation means is expanded to that. Which word and form match never
// depends on the expansion, only the suggestion does.
func (vocabulary *vocabulary) findMeaning(name string, meanings map[string]string) (finding, bool) {
	expansionOf := func(entry *vocabularyEntry) string {
		if meaning, found := meanings[entry.Abbreviation]; found {
			return meaning
		}
		return entry.singleExpansion()
	}
	if vocabulary.allowedNames[name] {
		return finding{}, false
	}
	if entry, found := vocabulary.wholeByName[name]; found {
		suggestion := expansionOf(entry)
		if entry.Whole.Style == "advice" {
			suggestion = ""
		}
		return finding{form: "whole", entry: entry, suggestion: suggestion}, true
	}
	prefixFinding := func(entry *vocabularyEntry) finding {
		if entry.Prefix.Style == "advice" || expansionOf(entry) == "" {
			return finding{form: "prefix", entry: entry}
		}
		return finding{
			form:       "prefix",
			entry:      entry,
			suggestion: expansionOf(entry) + strings.TrimPrefix(name, entry.Abbreviation),
		}
	}
	for _, entry := range vocabulary.earlyPrefixes {
		if vocabulary.allowedSegmentHolds(name, entry.Abbreviation) {
			continue
		}
		if vocabulary.prefixPatterns[entry.Abbreviation].MatchString(name) {
			return prefixFinding(entry), true
		}
	}
	for _, entry := range vocabulary.suffixes {
		if entry.Suffix.Matcher == "millisecondWord" {
			if millisecondPattern.MatchString(name) {
				suggestion := replaceFirst(millisecondPattern, name, "${1}"+entry.Suffix.Replacement+"${2}")
				return finding{form: "suffix", entry: entry, suggestion: suggestion}, true
			}
			continue
		}
		suffix := capitalize(entry.Abbreviation)
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		if entry.Suffix.Advice != "" {
			return finding{form: "suffix", entry: entry}, true
		}
		replacement := entry.Suffix.Replacement
		if replacement == "" {
			if expansionOf(entry) == "" {
				return finding{form: "suffix", entry: entry}, true
			}
			replacement = capitalize(expansionOf(entry))
		}
		return finding{form: "suffix", entry: entry, suggestion: strings.TrimSuffix(name, suffix) + replacement}, true
	}
	for _, entry := range vocabulary.latePrefixes {
		if vocabulary.prefixPatterns[entry.Abbreviation].MatchString(name) {
			return prefixFinding(entry), true
		}
	}
	if vocabulary.containsAllowedSegment(name) {
		return finding{}, false
	}
	for _, entry := range vocabulary.segments {
		patterns := vocabulary.segmentPatterns[entry.Abbreviation]
		if !patterns.boundary.MatchString(name) && !patterns.camel.MatchString(name) {
			continue
		}
		if entry.Segment.Style == "advice" || expansionOf(entry) == "" {
			return finding{form: "segment", entry: entry}, true
		}
		suggestion := replaceFirst(patterns.replace, name, capitalize(expansionOf(entry))+"${1}")
		if suggestion == name {
			continue
		}
		return finding{form: "segment", entry: entry, suggestion: suggestion}, true
	}
	return finding{}, false
}

// containsAllowedSegment reports whether a name holds a word that merely contains an abbreviation's
// letters, like `InnoDb`.
func (vocabulary *vocabulary) containsAllowedSegment(name string) bool {
	for _, segment := range vocabulary.allowedSegments {
		if strings.Contains(name, segment) {
			return true
		}
	}
	return false
}

// allowedSegmentHolds reports whether an allowed segment in the name holds this abbreviation's letters.
func (vocabulary *vocabulary) allowedSegmentHolds(name string, abbreviation string) bool {
	for _, segment := range vocabulary.allowedSegments {
		if strings.Contains(name, segment) && strings.Contains(strings.ToLower(segment), abbreviation) {
			return true
		}
	}
	return false
}

// maximumExpansions bounds the loop in expand. No real name holds this many words; a name still changing
// after it is a vocabulary whose expansions feed each other, which is a wave, not a rename.
const maximumExpansions = 8

// expansion is a name with every vocabulary word expanded, or why it cannot be.
type expansion struct {
	newName string
	// words are the abbreviations expanded, in the order the rule would report them.
	words []string
	// wave is non-empty when the name is left for a wave, and says why.
	wave string
}

// typeJudgedWords are the words whose expansion names a kind of value, so the local's type can say whether
// the word means what the vocabulary says, and what it means when it does not. In the ported lexers `ch` is a
// character, `res` a result and `reqSpace` a required space, and `channel`, `response` and `request` would
// misname them.
var typeJudgedWords = map[string]typeJudgedWord{
	"ch": {meaning: "channel", confirms: isChannel, otherwise: characterMeaning},
	"res": {
		meaning:   "response",
		confirms:  namesType("Response"),
		otherwise: func(types.Type) string { return "result" },
	},
	"req": {meaning: "request", confirms: namesType("Request"), otherwise: requirementMeaning},
}

// expand applies the rule's finding to the name until the rule finds nothing, so the result is a name the
// rule no longer flags. A word whose form gives advice, or an `err` or `ctx` word, stops it. A word the
// local's type judges (typeJudgedWords) takes the meaning the type gives it, and stops it when the type
// gives none.
func (vocabulary *vocabulary) expand(name string, localType types.Type) expansion {
	current := name
	var words []string
	meanings := map[string]string{}
	for range maximumExpansions {
		found, matched := vocabulary.findMeaning(current, meanings)
		if !matched {
			return expansion{newName: current, words: words}
		}
		if found.entry.Abbreviation == "err" || found.entry.Abbreviation == "ctx" {
			return expansion{words: words, wave: "holds err or ctx, which follow their own sections"}
		}
		if judged, isJudged := typeJudgedWords[found.entry.Abbreviation]; isJudged && localType != nil {
			if _, decided := meanings[found.entry.Abbreviation]; !decided && !judged.confirms(localType) {
				meaning := judged.otherwise(localType)
				if meaning == "" {
					return expansion{
						words: words,
						wave: fmt.Sprintf("the type %s says %q is not a %s, and names nothing else",
							localType, found.entry.Abbreviation, judged.meaning),
					}
				}
				meanings[found.entry.Abbreviation] = meaning
				found, _ = vocabulary.findMeaning(current, meanings)
			}
		}
		if found.suggestion == "" && len(found.entry.Expansions) > 1 {
			return expansion{
				words: words,
				wave: fmt.Sprintf("%q has several meanings (%s), so a person names it (%s form)",
					found.entry.Abbreviation, strings.Join(found.entry.Expansions, ", "), found.form),
			}
		}
		if found.suggestion == "" {
			return expansion{
				words: words,
				wave:  fmt.Sprintf("advice-only word %q (%s form)", found.entry.Abbreviation, found.form),
			}
		}
		words = append(words, found.entry.Abbreviation)
		if found.entry.Abbreviation == "num" && found.form == "prefix" {
			counted, wave := countName(strings.TrimPrefix(current, "num"))
			if wave != "" {
				return expansion{words: words, wave: wave}
			}
			if counted != "" {
				current = counted
				continue
			}
		}
		current = found.suggestion
	}
	return expansion{words: words, wave: fmt.Sprintf("still changing after %d expansions", maximumExpansions)}
}

// countName is the ruling for `num` before a plural: the singular plus `Count`, so `numLines` becomes
// `lineCount`, `numQuasis` `quasiCount` and `numEntries` `entryCount`. It returns no name and no wave when
// the rest is not a plural (`numError` takes the plain expansion), and a wave for a plural it cannot
// singularize by rule: an `-es` after ch, sh, s, x or z is `matches` as often as `caches`, and a rest
// opening with an acronym has no lower camel first word.
func countName(rest string) (string, string) {
	if !strings.HasSuffix(rest, "s") || strings.HasSuffix(rest, "ss") || strings.HasSuffix(rest, "us") {
		return "", ""
	}
	if len(rest) < 2 || !isUppercaseLetter(rest[0]) || isUppercaseLetter(rest[1]) {
		return "", "num before a plural that opens with an acronym"
	}
	singular := strings.TrimSuffix(rest, "s")
	switch {
	case strings.HasSuffix(rest, "ies"):
		singular = strings.TrimSuffix(rest, "ies") + "y"
	case strings.HasSuffix(rest, "es"):
		for _, ending := range []string{"ches", "shes", "ses", "xes", "zes"} {
			if strings.HasSuffix(rest, ending) {
				return "", "num before a plural in -es, which no rule singularizes"
			}
		}
	}
	return strings.ToLower(singular[:1]) + singular[1:] + "Count", ""
}

func isUppercaseLetter(character byte) bool { return character >= 'A' && character <= 'Z' }

func isLowercaseLetter(character byte) bool { return character >= 'a' && character <= 'z' }

func isDigit(character byte) bool { return character >= '0' && character <= '9' }

// camelWords splits a name into its camel words: a word starts at a capital after a lowercase letter or a
// digit, and at the last capital of an acronym run that a lowercase letter follows (`HTTPRequest` is HTTP,
// Request).
func camelWords(name string) []string {
	var words []string
	start := 0
	for index := 1; index < len(name); index++ {
		previous, current := name[index-1], name[index]
		if !isUppercaseLetter(current) {
			continue
		}
		afterLower := isLowercaseLetter(previous) || isDigit(previous)
		endsAcronym := isUppercaseLetter(previous) && index+1 < len(name) && isLowercaseLetter(name[index+1])
		if afterLower || endsAcronym {
			words = append(words, name[start:index])
			start = index
		}
	}
	return append(words, name[start:])
}

// englishShortWords are the two-letter camel words that are English words rather than fragments. A name
// keeps them through its expansion as written: `isVar` becomes `isVariable`, `declinedByConfig`
// `declinedByConfiguration`.
var englishShortWords = map[string]bool{
	"is": true, "as": true, "to": true, "by": true, "of": true, "at": true, "in": true, "on": true, "or": true,
	"if": true, "up": true, "no": true, "an": true, "do": true,
}

// keptShortWords returns the camel words of two letters or fewer that the name keeps through its expansion.
// The words the expansion replaces are not kept, so `elapsedMs` keeps none while `maxI` keeps `I` and
// `cePos` keeps `ce`.
func keptShortWords(name string, expandedWords []string) []string {
	expandedSet := map[string]bool{}
	for _, word := range expandedWords {
		expandedSet[word] = true
	}
	var kept []string
	for _, word := range camelWords(name) {
		if len(word) <= 2 && !expandedSet[strings.ToLower(word)] {
			kept = append(kept, word)
		}
	}
	return kept
}

// namesMetBefore is the name of every variable the function that declares variable declares before it, its
// literals included: the scopes from the function's own down, read up to variable's own declaration.
func namesMetBefore(variable *types.Var, packageScope *types.Scope) map[string]bool {
	scope := variable.Parent()
	// A function's scope is the one whose parent is a file scope, and a file scope's parent is the package's.
	for scope.Parent() != nil && scope.Parent().Parent() != packageScope {
		scope = scope.Parent()
	}
	names := map[string]bool{}
	var visit func(scope *types.Scope)
	visit = func(scope *types.Scope) {
		for _, name := range scope.Names() {
			if other, isVariable := scope.Lookup(name).(*types.Var); isVariable && other.Pos() < variable.Pos() {
				names[name] = true
			}
		}
		for child := range scope.Children() {
			visit(child)
		}
	}
	visit(scope)
	return names
}

// unheldShortWord returns the first kept short word that says nothing a reader can follow, or "". A short
// word is held when it is an English word, when it is the name of a variable the reader has already met in
// the function (`cePos` is the position of the `ce` declared above it), or when it is one capital letter
// that ends the name of such a variable, marking one of a pair (`refIndexA` after `indexA`). `maxK` keeps a
// `K` the function never names, and `maxI` an `I` whose `i` is declared only after it, so the two stay a pair
// for a wave rather than one reading `maximumI` beside a `maxK`.
func unheldShortWord(ownName string, kept []string, functionNames func() map[string]bool) string {
	for _, word := range kept {
		lowered := strings.ToLower(word)
		if englishShortWords[lowered] {
			continue
		}
		names := functionNames()
		if lowered != ownName && names[lowered] {
			continue
		}
		pairMarker := false
		if len(word) == 1 && isUppercaseLetter(word[0]) {
			for name := range names {
				if name != ownName &&
					len(name) > 1 &&
					strings.HasSuffix(name, word) &&
					!isUppercaseLetter(name[len(name)-2]) {
					pairMarker = true
				}
			}
		}
		if !pairMarker {
			return word
		}
	}
	return ""
}

// Outcomes for a local whose name holds a vocabulary word.
const (
	outcomeProposed = "proposed"
	// otherGeneratorPrefix marks a local another generator owns; it is neither proposed nor a wave.
	otherGeneratorPrefix = "other generator: "
	wavePrefix           = "wave: "
)

// What chose a proposed name, for the count.
const (
	choiceVocabulary  = "the vocabulary's expansion"
	choiceTypeMeaning = "the vocabulary with the meaning the type gives a word"
)

// choice is one name a local may take, and the rule that chose it.
type choice struct {
	name string
	rule string
}

// localName is one local whose name the vocabulary flags, and what this command does with it.
type localName struct {
	object   types.Object
	position token.Position
	newName  string
	outcome  string
	// rule says what chose newName.
	rule string
	// alternatives are the qualified names to try, in order, when the engine refuses newName for a conflict.
	alternatives []choice
}

// nonLocalName is one declared name that is not a local and that the vocabulary flags.
type nonLocalName struct {
	position token.Position
	kind     string
	name     string
}

// errorType is the predeclared error.
var errorType = types.Universe.Lookup("error").Type()

// typeJudgedWord is what the vocabulary says a word means, whether a type agrees, and what the word means in
// a local whose type does not, or "" when the type says nothing a rule can name.
type typeJudgedWord struct {
	meaning   string
	confirms  func(types.Type) bool
	otherwise func(types.Type) string
}

func isChannel(localType types.Type) bool {
	_, isChannel := localType.Underlying().(*types.Chan)
	return isChannel
}

// namesType confirms a type whose spelling holds word, as `*http.Request` holds Request.
func namesType(word string) func(types.Type) bool {
	return func(localType types.Type) bool { return strings.Contains(localType.String(), word) }
}

// characterMeaning is what a `ch` that is not a channel holds, read from its type: a uint16 is one UTF-16
// code unit, `codeUnit`; a rune, a byte, or an int holding a character code (the ported lexers' characterAt
// returns one, or -1 at the end) is a `character`, as the tree already names each of those types.
func characterMeaning(localType types.Type) string {
	basic, isBasic := localType.Underlying().(*types.Basic)
	if !isBasic {
		return ""
	}
	switch basic.Kind() {
	case types.Uint16:
		return "codeUnit"
	case types.Int32, types.Uint8, types.Int:
		return "character"
	}
	return ""
}

// requirementMeaning is what a `req` that is not a request means in a bool: whether something is required,
// so `reqSpace` reads `requiresSpace`.
func requirementMeaning(localType types.Type) string {
	if basic, isBasic := localType.Underlying().(*types.Basic); isBasic && basic.Kind() == types.Bool {
		return "requires"
	}
	return ""
}

// blockedReason says why a name cannot be a local's in a file with importNames, or "".
func blockedReason(name string, importNames map[string]bool) string {
	switch {
	case token.IsKeyword(name):
		return "would become a Go keyword"
	case types.Universe.Lookup(name) != nil:
		return "would become a predeclared identifier"
	case importNames[name]:
		return "would become the name of an import in the file"
	}
	return ""
}

// qualifierCall is a function whose result has a name the tree already gives it.
type qualifierCall struct {
	packagePath string
	name        string
}

// qualifierCalls name a local by the call that produced it, when its expansion is taken: the decoded options
// of a lint rule are its `settings`, as the tree writes them some eighty times.
var qualifierCalls = map[qualifierCall]string{
	{packagePath: "github.com/system-inc/cohere/internal/lint/rule", name: "OptionsAs"}: "settings",
}

// basicQualifier is a blocked expansion in a local of a basic type.
type basicQualifier struct {
	expansion string
	kind      types.BasicKind
}

// basicQualifiers name a local of a basic type, which has no type name to qualify it with, by what the value
// is: a string that would be `string` is `text`, and one that would be `package` holds a package's path.
var basicQualifiers = map[basicQualifier]string{
	{expansion: "string", kind: types.String}:  "text",
	{expansion: "package", kind: types.String}: "packagePath",
}

// Rules a qualified name comes from, in the order they are tried.
const (
	qualifierFromCall       = "the call that produced it"
	qualifierFromRange      = "what it ranges over"
	qualifierFromTypeName   = "its type's name"
	qualifierFromTypeOwner  = "its type's name after the directory that holds the type's package"
	qualifierFromBasicType  = "what a value of its basic type holds"
	qualifierFromNextOfName = "one past the value it is named for"
)

// namedTypeOf is the named type `resolvedType` is or points to, or nil.
func namedTypeOf(resolvedType types.Type) *types.TypeName {
	dereferenced := resolvedType
	if pointer, isPointer := types.Unalias(resolvedType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	if named, isNamed := types.Unalias(dereferenced).(*types.Named); isNamed && named.Obj().Pkg() != nil {
		return named.Obj()
	}
	return nil
}

// houseInitialisms writes every initialism after a name's first word as a word, the house's casing that
// consistency-no-uppercase-initialism asks for: `isURLArgument` is `isUrlArgument`. A renamed name is written
// the way the house writes names, so the rename never trades one finding for another.
func houseInitialisms(name string) string {
	words := camelWords(name)
	for index := 1; index < len(words); index++ {
		word := words[index]
		if len(word) >= 2 && strings.ToUpper(word) == word && strings.ToLower(word) != word {
			words[index] = word[:1] + strings.ToLower(word[1:])
		}
	}
	return strings.Join(words, "")
}

// lowerCamel lowers a name's first camel word whole, initialism or not: `ParsedCommandLine` is
// `parsedCommandLine`, `URLPattern` `urlPattern`.
// folderWords is a folder's name as the lowerCamel words of a Go name: a folder separates its words with
// underscores (go_engine), and a Go name never does, so go_engine gives goEngine.
func folderWords(folder string) string {
	words := strings.Split(folder, "_")
	for index := 1; index < len(words); index++ {
		words[index] = capitalize(words[index])
	}
	return strings.Join(words, "")
}

func lowerCamel(name string) string {
	words := camelWords(name)
	words[0] = strings.ToLower(words[0])
	return strings.Join(words, "")
}

// qualifiedNames lists the names a local takes, in order, when its expansion is blocked or conflicts, each from
// its type or its use, never a numbered suffix:
//
//  1. the name the tree gives the result of the call that defined it (qualifierCalls);
//  2. for a basic type, what the value holds (basicQualifiers);
//  3. for a range value, one of what it ranges over: `range lintPackages(...)` gives `lintPackage`, and
//     `range loaded.Source` `sourcePackage`;
//  4. its named type's name in full words (`pos` of type lineAndColumn is `lineAndColumn`);
//  5. that name after the directory that holds the type's package, which says whose it is
//     (`config` of type *configuration.Config, in internal/lint/configuration, is `lintConfiguration`);
//     `internal` is Go's visibility marker and says nothing, so it never qualifies;
//  6. for a value defined one past a value named for the expansion, `next` and the expansion
//     (`pos := parser.position + 1` is `nextPosition`).
//
// A name is listed only if the vocabulary leaves it alone and it is not blocked in the file.
func qualifiedNames(
	vocabulary *vocabulary,
	info *types.Info,
	variable *types.Var,
	blocked string,
	definition ast.Expr,
	rangedOver ast.Expr,
	importNames map[string]bool,
) []choice {
	var choices []choice
	seen := map[string]bool{variable.Name(): true, blocked: true}
	add := func(name string, rule string) {
		if name == "" || seen[name] || !token.IsIdentifier(name) || blockedReason(name, importNames) != "" {
			return
		}
		if _, flagged := vocabulary.find(name); flagged {
			return
		}
		seen[name] = true
		choices = append(choices, choice{name: name, rule: rule})
	}
	if call, isCall := ast.Unparen(definition).(*ast.CallExpr); isCall {
		if function, isFunction := typeutil.Callee(info, call).(*types.Func); isFunction && function.Pkg() != nil {
			function = function.Origin()
			add(
				qualifierCalls[qualifierCall{packagePath: function.Pkg().Path(), name: function.Name()}],
				qualifierFromCall,
			)
		}
	}
	if basic, isBasic := variable.Type().Underlying().(*types.Basic); isBasic {
		add(basicQualifiers[basicQualifier{expansion: blocked, kind: basic.Kind()}], qualifierFromBasicType)
	}
	if rangedName := expressionName(rangedOver); rangedName != "" {
		plural := capitalize(blocked) + "s"
		switch {
		case strings.HasSuffix(rangedName, plural) && rangedName != plural:
			add(lowerCamel(strings.TrimSuffix(rangedName, "s")), qualifierFromRange)
		case !strings.EqualFold(rangedName, plural):
			add(lowerCamel(rangedName)+capitalize(blocked), qualifierFromRange)
		}
	}
	if typeName := namedTypeOf(variable.Type()); typeName != nil {
		typeWords := vocabulary.expand(lowerCamel(typeName.Name()), nil)
		if typeWords.wave == "" {
			add(typeWords.newName, qualifierFromTypeName)
			pathElements := strings.Split(typeName.Pkg().Path(), "/")
			if len(pathElements) >= 2 {
				owner := pathElements[len(pathElements)-2]
				if owner != "internal" && owner != "" && strings.ToLower(owner) == owner {
					add(folderWords(owner)+capitalize(typeWords.newName), qualifierFromTypeOwner)
				}
			}
		}
	}
	if sum, isSum := ast.Unparen(definition).(*ast.BinaryExpr); isSum && sum.Op == token.ADD {
		literal, isLiteral := ast.Unparen(sum.Y).(*ast.BasicLit)
		var operandName string
		switch operand := ast.Unparen(sum.X).(type) {
		case *ast.Ident:
			operandName = operand.Name
		case *ast.SelectorExpr:
			operandName = operand.Sel.Name
		}
		if isLiteral && literal.Value == "1" && operandName == blocked {
			add("next"+capitalize(blocked), qualifierFromNextOfName)
		}
	}
	return choices
}

// expressionName is the name an expression reads as: an identifier's, a selector's last name, or the name of
// the function a call calls. It is "" for anything else.
func expressionName(expression ast.Expr) string {
	switch typed := ast.Unparen(expression).(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.CallExpr:
		return expressionName(typed.Fun)
	}
	return ""
}

// definitionsOf maps each variable a file defines with a value to that value: the matching right side, or the
// one call that defines several (`settings, ok := rule.OptionsAs[T](options)`). It maps each range value it
// defines to what the range ranges over.
func definitionsOf(info *types.Info, file *ast.File) (map[*types.Var]ast.Expr, map[*types.Var]ast.Expr) {
	definitions := map[*types.Var]ast.Expr{}
	rangedOver := map[*types.Var]ast.Expr{}
	record := func(names []*ast.Ident, values []ast.Expr) {
		for index, name := range names {
			variable, isVariable := info.Defs[name].(*types.Var)
			if !isVariable {
				continue
			}
			switch {
			case len(values) == len(names):
				definitions[variable] = values[index]
			case len(values) == 1:
				definitions[variable] = values[0]
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			if typed.Tok == token.DEFINE {
				var names []*ast.Ident
				for _, left := range typed.Lhs {
					identifier, _ := left.(*ast.Ident)
					names = append(names, identifier)
				}
				record(names, typed.Rhs)
			}
		case *ast.ValueSpec:
			record(typed.Names, typed.Values)
		case *ast.RangeStmt:
			if typed.Tok != token.DEFINE {
				break
			}
			if value, isIdentifier := typed.Value.(*ast.Ident); isIdentifier {
				if variable, isVariable := info.Defs[value].(*types.Var); isVariable {
					rangedOver[variable] = typed.X
				}
			}
		}
		return true
	})
	return definitions, rangedOver
}

// planPackage finds every local in the editable files whose name the vocabulary flags, decides the name for
// those it can, and counts the flagged names that are not locals. A proposed local carries the qualified
// names to try if the engine refuses its name for a conflict.
func planPackage(renamePackage rename.Package, vocabulary *vocabulary) ([]localName, []nonLocalName) {
	var locals []localName
	var nonLocals []nonLocalName
	for _, file := range renamePackage.Files {
		receivers := map[types.Object]bool{}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv == nil {
				continue
			}
			for _, field := range function.Recv.List {
				for _, name := range field.Names {
					receivers[renamePackage.Info.Defs[name]] = true
				}
			}
		}
		importNames := map[string]bool{}
		for _, importSpecification := range file.Imports {
			if packageName := renamePackage.Info.PkgNameOf(importSpecification); packageName != nil {
				importNames[packageName.Name()] = true
			}
		}
		definitions, rangedOver := definitionsOf(renamePackage.Info, file)

		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier || identifier.Name == "_" {
				return true
			}
			object := renamePackage.Info.Defs[identifier]
			if object == nil {
				return true
			}
			if _, flagged := vocabulary.find(identifier.Name); !flagged {
				return true
			}
			position := renamePackage.FileSet.PositionFor(identifier.Pos(), false)
			variable, isVariable := object.(*types.Var)
			if !isVariable || !rename.IsLocal(variable, renamePackage.Types) {
				nonLocals = append(nonLocals, nonLocalName{
					position: position,
					kind:     nonLocalKind(object, renamePackage.Types),
					name:     identifier.Name,
				})
				return true
			}
			local := localName{object: variable, position: position}
			expanded := vocabulary.expand(identifier.Name, variable.Type())
			functionNames := func() map[string]bool { return namesMetBefore(variable, renamePackage.Types.Scope()) }
			unheld := unheldShortWord(identifier.Name, keptShortWords(identifier.Name, expanded.words), functionNames)
			rule := choiceVocabulary
			if vocabulary.expand(identifier.Name, nil).newName != expanded.newName {
				rule = choiceTypeMeaning
			}
			switch {
			case receivers[variable]:
				local.outcome = otherGeneratorPrefix + "receiver"
			case identifier.Name == "err" || identifier.Name == "ctx":
				local.outcome = otherGeneratorPrefix + "`" + identifier.Name + "`"
			case types.Identical(variable.Type(), errorType):
				local.outcome = otherGeneratorPrefix + "a local of type error"
			case expanded.wave != "":
				local.outcome = wavePrefix + expanded.wave
			case unheld != "":
				local.outcome = wavePrefix +
					fmt.Sprintf("keeps the short camel word %q, which names nothing in its function", unheld)
			case expanded.newName == identifier.Name:
				// The rule flags the name and every step left it as it was; find never returns such a
				// suggestion, so this is a guard, reported rather than silently dropped.
				local.outcome = wavePrefix + "expansion leaves the name unchanged"
			default:
				expanded.newName = houseInitialisms(expanded.newName)
				qualified := qualifiedNames(
					vocabulary,
					renamePackage.Info,
					variable,
					expanded.newName,
					definitions[variable],
					rangedOver[variable],
					importNames,
				)
				blocked := blockedReason(expanded.newName, importNames)
				switch {
				case blocked == "":
					local.outcome = outcomeProposed
					local.newName, local.rule, local.alternatives = expanded.newName, rule, qualified
				case len(qualified) == 0:
					local.outcome = wavePrefix + blocked + ", and no qualifier rule names it"
				default:
					local.outcome = outcomeProposed
					local.newName, local.rule, local.alternatives = qualified[0].name, qualified[0].rule+
						" ("+
						blocked+
						")", qualified[1:]
				}
			}
			if local.newName == "" {
				local.newName = expanded.newName
			}
			locals = append(locals, local)
			return true
		})
	}
	return locals, nonLocals
}

// checkQualified runs the engine over every proposed local, and gives each one it refuses for a conflict its
// next qualified name, round after round, until a round gives none a new name. It returns the last round's
// plan, and for each accepted local the rule that chose its name.
func checkQualified(renamePackage rename.Package, locals []localName) (rename.Plan, map[types.Object]string) {
	choices := map[types.Object][]choice{}
	var order []types.Object
	for _, local := range locals {
		if local.outcome != outcomeProposed {
			continue
		}
		if _, seen := choices[local.object]; !seen {
			order = append(order, local.object)
		}
		choices[local.object] = append([]choice{{name: local.newName, rule: local.rule}}, local.alternatives...)
	}
	current := map[types.Object]int{}
	for {
		var proposals []rename.Proposal
		for _, object := range order {
			proposals = append(
				proposals,
				rename.Proposal{Object: object, NewName: choices[object][current[object]].name},
			)
		}
		plan := rename.Check(renamePackage, proposals)
		advanced := false
		for _, rejection := range plan.Rejections {
			if strings.HasPrefix(rejection.Reason, "conflict:") &&
				current[rejection.Object]+1 < len(choices[rejection.Object]) {
				current[rejection.Object]++
				advanced = true
			}
		}
		if !advanced {
			rules := map[types.Object]string{}
			for object := range plan.Accepted {
				rules[object] = choices[object][current[object]].rule
				if current[object] > 0 {
					rules[object] += " (after a conflict)"
				}
			}
			return plan, rules
		}
	}
}

// nonLocalKind names what a declared object that is not a local variable is, for the count.
func nonLocalKind(object types.Object, packageTypes *types.Package) string {
	atPackageLevel := object.Parent() == packageTypes.Scope()
	switch typed := object.(type) {
	case *types.Var:
		if typed.IsField() {
			return "struct field"
		}
		if atPackageLevel {
			return "package variable"
		}
		return "variable outside a function"
	case *types.Func:
		if signature, isSignature := typed.Type().(*types.Signature); isSignature && signature.Recv() != nil {
			return "method"
		}
		return "function"
	case *types.Const:
		if atPackageLevel {
			return "package constant"
		}
		return "local constant"
	case *types.TypeName:
		if _, isParameter := typed.Type().(*types.TypeParam); isParameter {
			return "type parameter"
		}
		if atPackageLevel {
			return "type"
		}
		return "local type"
	case *types.PkgName:
		return "import name"
	case *types.Label:
		return "label"
	}
	return fmt.Sprintf("%T", object)
}

func run(root string, vocabularyPath string, skips []string, dryRun bool) int {
	vocabularyData, readError := readVocabulary(vocabularyPath)
	if readError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: reading the vocabulary: %v\n", readError)
		return 1
	}
	loadedVocabulary, vocabularyError := loadVocabulary(vocabularyData)
	if vocabularyError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: %s: %v\n", vocabularyPath, vocabularyError)
		return 1
	}
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "wordnames: loading: %v\n", loadError)
		return 1
	}
	fmt.Fprintf(
		streams.Output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(streams.Output)
	editableFiles, skippedFiles := 0, 0
	for _, variant := range loaded.Analyzed {
		editable := len(loaded.RenamePackage(variant).Files)
		editableFiles += editable
		skippedFiles += len(variant.Syntax) - editable
	}
	fmt.Fprintf(streams.Output, "files in analyzed variants: %d editable, %d skipped\n", editableFiles, skippedFiles)
	if editableFiles == 0 {
		fmt.Fprintln(streams.Errors, "wordnames: no editable files; the root or skip rules are wrong")
		return 1
	}

	positionKey := func(position token.Position) string {
		return fmt.Sprintf("%s:%d", position.Filename, position.Offset)
	}
	localsByKey := map[string]localName{}
	nonLocalsByKey := map[string]nonLocalName{}
	acceptedByKey := map[string]localName{}
	var allEdits []rename.Edit
	var allRejections []rename.Rejection
	var allUnresolved []string
	for _, variant := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(variant)
		locals, nonLocals := planPackage(renamePackage, loadedVocabulary)
		for _, local := range locals {
			localsByKey[positionKey(local.position)] = local
		}
		for _, nonLocal := range nonLocals {
			nonLocalsByKey[positionKey(nonLocal.position)] = nonLocal
		}
		plan, rules := checkQualified(renamePackage, locals)
		allRejections = append(allRejections, plan.Rejections...)
		for _, unresolved := range plan.Unresolved {
			allUnresolved = append(allUnresolved, variant.ID+": "+unresolved)
		}
		for object, newName := range plan.Accepted {
			position := renamePackage.FileSet.PositionFor(object.Pos(), false)
			acceptedByKey[positionKey(
				position,
			)] = localName{object: object, position: position, newName: newName, outcome: outcomeProposed,
				rule: rules[object]}
		}
		allEdits = append(allEdits, rename.Edits(renamePackage, plan.Accepted)...)
	}
	edits, disagreements := rename.Deduplicate(allEdits)
	editsByFile := rename.GroupByFile(edits)

	outcomes := map[string]int{}
	proposed := 0
	for _, local := range localsByKey {
		outcomes[local.outcome]++
		if local.outcome == outcomeProposed {
			proposed++
		}
	}
	fmt.Fprintf(streams.Output, "locals whose name holds a vocabulary word: %d\n", len(localsByKey))
	fmt.Fprintf(streams.Output, "  proposed: %d\n", proposed)
	fmt.Fprintln(streams.Output, "  left to other generators:")
	for _, outcome := range sortedKeys(outcomes) {
		if strings.HasPrefix(outcome, otherGeneratorPrefix) {
			fmt.Fprintf(
				streams.Output,
				"    %s: %d\n",
				strings.TrimPrefix(outcome, otherGeneratorPrefix),
				outcomes[outcome],
			)
		}
	}
	fmt.Fprintln(streams.Output, "  waves:")
	for _, outcome := range sortedKeys(outcomes) {
		if strings.HasPrefix(outcome, wavePrefix) {
			fmt.Fprintf(streams.Output, "    %s: %d\n", strings.TrimPrefix(outcome, wavePrefix), outcomes[outcome])
		}
	}
	fmt.Fprintf(streams.Output, "accepted by the engine: %d\n", len(acceptedByKey))
	acceptedByRule := map[string]int{}
	for _, accepted := range acceptedByKey {
		acceptedByRule[accepted.rule]++
	}
	for _, rule := range sortedKeys(acceptedByRule) {
		fmt.Fprintf(streams.Output, "  named by %s: %d\n", rule, acceptedByRule[rule])
	}
	fmt.Fprintf(streams.Output, "rejected by the engine: %d\n", len(allRejections))
	rejectedByReason := map[string]int{}
	for _, rejection := range allRejections {
		rejectedByReason[reasonClass(rejection.Reason)]++
	}
	for _, reason := range sortedKeys(rejectedByReason) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", reason, rejectedByReason[reason])
	}
	fmt.Fprintf(streams.Output, "edits: %d in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(streams.Output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintf(streams.Output, "unresolved occurrences (scope model gaps): %d\n", len(allUnresolved))

	renameCounts := map[string]int{}
	for _, accepted := range acceptedByKey {
		renameCounts[accepted.object.Name()+" -> "+accepted.newName]++
	}
	fmt.Fprintln(streams.Output, "\nmost frequent renames:")
	printTop(renameCounts, 20, "  ")

	nonLocalCounts := map[string]int{}
	nonLocalNamesByKind := map[string]map[string]int{}
	for _, nonLocal := range nonLocalsByKey {
		nonLocalCounts[nonLocal.kind]++
		if nonLocalNamesByKind[nonLocal.kind] == nil {
			nonLocalNamesByKind[nonLocal.kind] = map[string]int{}
		}
		nonLocalNamesByKind[nonLocal.kind][nonLocal.name]++
	}
	fmt.Fprintf(
		streams.Output,
		"\nnon-local names holding a vocabulary word (counted, not proposed): %d\n",
		len(nonLocalsByKey),
	)
	for _, kind := range sortedKeys(nonLocalCounts) {
		fmt.Fprintf(
			streams.Output,
			"  %s: %d, %d distinct\n",
			kind,
			nonLocalCounts[kind],
			len(nonLocalNamesByKind[kind]),
		)
		printTop(nonLocalNamesByKind[kind], 10, "    ")
	}

	acceptedKeys := sortedKeys(acceptedByKey)
	fmt.Fprintln(streams.Output, "\nsamples in context:")
	for index := range min(10, len(acceptedKeys)) {
		accepted := acceptedByKey[acceptedKeys[index*len(acceptedKeys)/min(10, len(acceptedKeys))]]
		before, after := lineInContext(accepted.position, editsByFile[accepted.position.Filename])
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s\n    before: %s\n    after:  %s\n",
			relativePosition(loaded, accepted.position),
			accepted.object.Name(),
			accepted.newName,
			before,
			after,
		)
	}

	fmt.Fprintln(streams.Output, "\naccepted renames:")
	for _, key := range acceptedKeys {
		accepted := acceptedByKey[key]
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s [%s]\n",
			relativePosition(loaded, accepted.position),
			accepted.object.Name(),
			accepted.newName,
			accepted.rule,
		)
	}
	fmt.Fprintln(streams.Output, "\nrejections:")
	for _, rejection := range allRejections {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s: %s\n",
			relativePosition(loaded, rejection.Position),
			rejection.Object.Name(),
			rejection.NewName,
			strings.ReplaceAll(rejection.Reason, loaded.Root+"/", ""),
		)
	}
	fmt.Fprintln(streams.Output, "\nwaves:")
	for _, key := range sortedKeys(localsByKey) {
		local := localsByKey[key]
		if !strings.HasPrefix(local.outcome, wavePrefix) {
			continue
		}
		suggestion := ""
		if local.newName != "" && local.newName != local.object.Name() {
			suggestion = " (would be " + local.newName + ")"
		}
		fmt.Fprintf(
			streams.Output,
			"  %s: %s%s: %s\n",
			relativePosition(loaded, local.position),
			local.object.Name(),
			suggestion,
			strings.TrimPrefix(local.outcome, wavePrefix),
		)
	}
	fmt.Fprintln(streams.Output, "\nnon-local names:")
	for _, key := range sortedKeys(nonLocalsByKey) {
		nonLocal := nonLocalsByKey[key]
		fmt.Fprintf(
			streams.Output,
			"  %s: %s %s\n",
			relativePosition(loaded, nonLocal.position),
			nonLocal.kind,
			nonLocal.name,
		)
	}
	fmt.Fprintln(streams.Output, "\nvariant disagreements:")
	for _, disagreement := range disagreements {
		fmt.Fprintln(streams.Output, "  "+disagreement)
	}
	fmt.Fprintln(streams.Output, "\nunresolved occurrences:")
	for _, unresolved := range allUnresolved {
		fmt.Fprintln(streams.Output, "  "+strings.ReplaceAll(unresolved, loaded.Root+"/", ""))
	}

	if len(disagreements) > 0 {
		fmt.Fprintln(streams.Errors, "wordnames: variants disagree on some edits; those spans were dropped")
	}
	if dryRun {
		return 0
	}
	return apply(loaded, root, skips, edits, editsByFile)
}

// apply hands the edits to workspace.ApplyVerified, which proves the rewrite in memory and writes only a
// proven result.
func apply(
	loaded *workspace.Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
) int {
	return workspace.ApplyVerified(loaded, root, skips, edits, editsByFile, "wordnames", streams.Output, streams.Errors)
}

// lineInContext returns the source line holding position, as it is and with the file's edits on that line
// applied.
func lineInContext(position token.Position, fileEdits []rename.Edit) (string, string) {
	source, readError := os.ReadFile(position.Filename)
	if readError != nil {
		return readError.Error(), ""
	}
	lineStart := bytes.LastIndexByte(source[:position.Offset], '\n') + 1
	lineEnd := bytes.IndexByte(source[position.Offset:], '\n')
	if lineEnd < 0 {
		lineEnd = len(source)
	} else {
		lineEnd += position.Offset
	}
	line := string(source[lineStart:lineEnd])
	rewritten := line
	var onLine []rename.Edit
	for _, edit := range fileEdits {
		if edit.Offset >= lineStart && edit.Offset < lineEnd {
			onLine = append(onLine, edit)
		}
	}
	sort.Slice(onLine, func(left, right int) bool { return onLine[left].Offset > onLine[right].Offset })
	for _, edit := range onLine {
		start := edit.Offset - lineStart
		rewritten = rewritten[:start] + edit.NewText + rewritten[start+len(edit.OldText):]
	}
	return strings.TrimSpace(line), strings.TrimSpace(rewritten)
}

// conflictKind finds the kind of object a conflict would resolve to in the engine's reason.
var conflictKind = regexp.MustCompile(`would resolve to (\S+) `)

// reasonClass groups rejection reasons for counting, a conflict by the kind of object it would hit.
func reasonClass(reason string) string {
	if strings.HasPrefix(reason, "conflict:") {
		if match := conflictKind.FindStringSubmatch(reason); match != nil {
			return "conflict with a " + match[1]
		}
		return "conflict"
	}
	return reason
}

// relativePosition prints a position relative to the root.
func relativePosition(loaded *workspace.Workspace, position token.Position) string {
	if relative, inside := loaded.Relative(position.Filename); inside {
		return fmt.Sprintf("%s:%d", relative, position.Line)
	}
	return position.String()
}

// printTop prints the most frequent keys of counts, ties by key.
func printTop(counts map[string]int, limit int, indent string) {
	keys := sortedKeys(counts)
	sort.SliceStable(keys, func(left, right int) bool { return counts[keys[left]] > counts[keys[right]] })
	for index, key := range keys {
		if index == limit {
			break
		}
		fmt.Fprintf(streams.Output, "%s%5d  %s\n", indent, counts[key], key)
	}
}

// sortedKeys returns a map's keys in order.
func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// readVocabulary is the vocabulary file at path, or the one built into the pass when path is empty: policy's
// Abbreviations.json from the cohere tree the pass was built in, so a pass run over another repository, which holds
// no policy of its own, judges words as it does over cohere.
func readVocabulary(path string) ([]byte, error) {
	if path == "" {
		return policy.AbbreviationsFile(), nil
	}
	return os.ReadFile(path)
}
