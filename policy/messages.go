package policy

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// The house rules' message text, one file per idea: a rule and its twin in the other language, or a
// lone house rule. Each engine renders its findings from these files, so the same idea reads the same in
// Go and Swift, and a fix to the wording lands once (#xd6f0n6, under #s05t0hs).
//
// A file holds the rules it speaks for, by language, and their messages by id. The id is one for both
// twins and is part of the 1.0 contract: the defect as a camelCase noun phrase (`bareThrow`), with no
// category or verb. A message's text carries two kinds of placeholder:
//
//	{{name}}   a value the rule supplies when it reports, in ESLint's spelling, so a message ported from
//	           an original keeps its text. Each engine supplies exactly the values its text uses.
//	[[name]]   a term: words that differ by language (`console` against `print`), resolved when the
//	           file is read. A term gives a text for every language the file's rules cover, and may
//	           carry values. Terms are the exception; a message with none reads the same in both.
//	<<name>>   a phrase from the file's `phrases`, which comes in two kinds. A string phrase is text two
//	           or more of the file's messages share, put in when the file is read, so a sentence the
//	           messages have in common lives once. An object phrase is a closed set of named options,
//	           and the rule picks one when it reports: wording that varies by case, kept in the data
//	           rather than passed in as a value. An option may carry values. An option's text is one
//	           string for every language, or an object giving it by language, as a term does; an option
//	           with no text for a language does not reach that language, so its rule cannot pick it.
//
// Nothing nests: a term holds no term or phrase, and a phrase holds no phrase or term. Swift's
// generator resolves string phrases when it generates, and an object phrase becomes an enum parameter
// of the message's typed function.
//
//go:embed messages/*.json
var messageFiles embed.FS

// The languages a message file speaks for, as its `rules` and its terms spell them.
const (
	MessageLanguageTypeScript = "TypeScript"
	MessageLanguageSwift      = "Swift"
	MessageLanguageGo         = "Go"
)

// messageLanguages are the languages a file may name.
var messageLanguages = map[string]bool{
	MessageLanguageTypeScript: true,
	MessageLanguageSwift:      true,
	MessageLanguageGo:         true,
}

// messageLanguagesNamingTheFile is the order a file's idea is read from its rules: the TypeScript rule, the
// original, then Swift's, then Go's, so a twin file keeps the name it had before Go joined it.
var messageLanguagesNamingTheFile = []string{MessageLanguageTypeScript, MessageLanguageSwift, MessageLanguageGo}

// messageFile is one file under messages/, as written.
type messageFile struct {
	Rules    map[string]string             `json:"rules"`
	Phrases  map[string]json.RawMessage    `json:"phrases"`
	Messages map[string]messageFileMessage `json:"messages"`
}

type messageFileMessage struct {
	Text  string                       `json:"text"`
	Terms map[string]map[string]string `json:"terms"`
	// Languages names the engines that render this message, when only some of the file's rules do: a
	// twin file can hold a message only one engine detects. Left out, it is every language the file
	// names.
	Languages []string `json:"languages"`
}

// messageTemplate is one message for one rule, its terms and string phrases resolved. Its object
// phrases are left as `<<name>>` slots, with their options here, filled when the rule reports.
type messageTemplate struct {
	text    string
	options map[string]map[string]string
}

// MessageOption is a rule's claim on one option of an object phrase its message uses, taken when the
// rule's package initializes like the handle, so an option no rule claims fails the unused-entry test.
type MessageOption struct {
	Rule   string
	Id     string
	Phrase string
	Name   string
}

// currentMessages is the catalog handles render from: the embedded files, unless a test swapped in an
// edited copy through UseMessages.
var currentMessages atomic.Pointer[MessageCatalog]

// requestedMessages records every handle taken, so a test can fail on an entry no rule renders.
var requestedMessages = struct {
	sync.Mutex
	handles map[MessageHandle]bool
	options map[MessageOption]bool
}{handles: map[MessageHandle]bool{}, options: map[MessageOption]bool{}}

// Messages is the embedded catalog, read once. An invalid file panics at startup: a message read
// wrongly would print the wrong words in every finding while every check reported the file clean.
var Messages = mustLoadEmbeddedMessages()

// MessageFiles is the embedded messages directory, its files at the root, for a test that edits one.
func MessageFiles() fs.FS {
	files, subError := fs.Sub(messageFiles, "messages")
	if subError != nil {
		// Invariant: fs.Sub fails only on an invalid path, and "messages" is a valid one, embedded by the
		// go:embed above.
		panic(fmt.Sprintf("policy/messages: %v", subError))
	}
	return files
}

func mustLoadEmbeddedMessages() *MessageCatalog {
	catalog, loadMessagesError := LoadMessages(MessageFiles())
	if loadMessagesError != nil {
		// Invariant: the files are embedded at build and read when the package initializes, so an invalid one
		// fails every test binary that links this package, and no build that passed its tests reaches here.
		panic(fmt.Sprintf("policy/messages: %v", loadMessagesError))
	}
	currentMessages.Store(catalog)
	return catalog
}

// LoadMessages reads every `.json` file at the root of files as a message file and checks it. It
// refuses an unknown key, a file named for something other than its idea, a rule named by two files,
// an id that is not camelCase, a placeholder that is malformed or names nothing, a term missing a
// language or given one the file does not speak for, a term no text uses, and a term on a lone rule.
func LoadMessages(files fs.FS) (*MessageCatalog, error) {
	names, globError := fs.Glob(files, "*.json")
	if globError != nil {
		return nil, globError
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no message files, so no house rule could render a finding")
	}
	catalog := &MessageCatalog{
		templates: map[string]map[string]messageTemplate{},
		languages: map[string]string{},
	}
	for _, name := range names {
		data, readFileError := fs.ReadFile(files, name)
		if readFileError != nil {
			return nil, readFileError
		}
		if addError := catalog.add(strings.TrimSuffix(path.Base(name), ".json"), data); addError != nil {
			return nil, fmt.Errorf("%s: %w", name, addError)
		}
	}
	return catalog, nil
}

// messageLanguagesOf is the languages a message is rendered in: its own list, every one a language the
// file names a rule for and none twice, or every language the file names.
func messageLanguagesOf(message messageFileMessage, rules map[string]string) ([]string, error) {
	if message.Languages == nil {
		languages := make([]string, 0, len(rules))
		for language := range rules {
			languages = append(languages, language)
		}
		sort.Strings(languages)
		return languages, nil
	}
	if len(message.Languages) == 0 {
		return nil, fmt.Errorf("languages is empty, so no engine renders it")
	}
	seen := map[string]bool{}
	for _, language := range message.Languages {
		if _, named := rules[language]; !named {
			return nil, fmt.Errorf("languages: %q is not a language the file names a rule for", language)
		}
		if seen[language] {
			return nil, fmt.Errorf("languages: %q appears twice", language)
		}
		seen[language] = true
	}
	return message.Languages, nil
}

// objectPhrase is an object phrase's options, each option's text by language. A text for every
// language is held under the empty language.
type objectPhrase map[string]map[string]string

// readPhrases splits a file's phrases into the string kind and the object kind, refusing a name that
// is not camelCase, an object with no option or an option name that is not, an option giving its text
// by language for no language or for one the file names no rule for, and a phrase holding a phrase or
// a term.
func readPhrases(
	raw map[string]json.RawMessage,
	rules map[string]string,
) (map[string]string, map[string]objectPhrase, error) {
	stringPhrases := map[string]string{}
	objectPhrases := map[string]objectPhrase{}
	for name, value := range raw {
		if !isCamelCaseName(name) {
			return nil, nil, fmt.Errorf("phrase %q: a phrase is named in camelCase", name)
		}
		var text string
		var texts []string
		if strictUnmarshalError := strictUnmarshal(value, &text); strictUnmarshalError == nil {
			stringPhrases[name] = text
			texts = append(texts, text)
		} else {
			var options map[string]json.RawMessage
			if strictUnmarshalError := strictUnmarshal(value, &options); strictUnmarshalError != nil {
				return nil, nil, fmt.Errorf("phrase %q is neither text nor named options of text", name)
			}
			if len(options) == 0 {
				return nil, nil, fmt.Errorf("phrase %q has no option", name)
			}
			phrase := objectPhrase{}
			for option, optionValue := range options {
				if !isCamelCaseName(option) {
					return nil, nil, fmt.Errorf("phrase %q: the option %q is not camelCase", name, option)
				}
				byLanguage, readOptionTextError := readOptionText(optionValue, rules)
				if readOptionTextError != nil {
					return nil, nil, fmt.Errorf("phrase %q, option %q: %w", name, option, readOptionTextError)
				}
				for _, optionText := range byLanguage {
					texts = append(texts, optionText)
				}
				phrase[option] = byLanguage
			}
			objectPhrases[name] = phrase
		}
		for _, text := range texts {
			if strings.Contains(text, "<<") ||
				strings.Contains(text, ">>") ||
				strings.Contains(text, "[[") ||
				strings.Contains(text, "]]") {
				return nil, nil, fmt.Errorf("phrase %q holds a phrase or a term, and nothing nests", name)
			}
			if _, placeholderNamesError := placeholderNames(text, "{{", "}}"); placeholderNamesError != nil {
				return nil, nil, fmt.Errorf("phrase %q: %w", name, placeholderNamesError)
			}
		}
	}
	return stringPhrases, objectPhrases, nil
}

// readOptionText reads one option's text: a string, which every language renders, or an object of
// text by language, which only the languages it names render.
func readOptionText(value json.RawMessage, rules map[string]string) (map[string]string, error) {
	var text string
	if strictUnmarshalError := strictUnmarshal(value, &text); strictUnmarshalError == nil {
		return map[string]string{"": text}, nil
	}
	var byLanguage map[string]string
	if strictUnmarshalError := strictUnmarshal(value, &byLanguage); strictUnmarshalError != nil {
		return nil, fmt.Errorf("is neither text nor text by language")
	}
	if len(byLanguage) == 0 {
		return nil, fmt.Errorf("gives its text for no language, so nothing renders it")
	}
	for language := range byLanguage {
		if _, named := rules[language]; !named {
			return nil, fmt.Errorf("gives text for %q, which the file names no rule for", language)
		}
	}
	return byLanguage, nil
}

// strictUnmarshal decodes JSON into target, refusing a value of another shape.
func strictUnmarshal[Target any](data []byte, target *Target) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// resolvePhrases puts each string phrase into text, and keeps each object phrase as a slot with the
// options that have text for language, refusing a `<<name>>` the file does not define and an object
// phrase with no option for the language the text is rendered in.
func resolvePhrases(
	text string,
	stringPhrases map[string]string,
	objectPhrases map[string]objectPhrase,
	language string,
) (messageTemplate, error) {
	names, placeholderNamesError := placeholderNames(text, "<<", ">>")
	if placeholderNamesError != nil {
		return messageTemplate{}, placeholderNamesError
	}
	template := messageTemplate{text: text}
	for _, name := range names {
		if phrase, isString := stringPhrases[name]; isString {
			template.text = strings.ReplaceAll(template.text, "<<"+name+">>", phrase)
			continue
		}
		phrase, isObject := objectPhrases[name]
		if !isObject {
			return messageTemplate{}, fmt.Errorf("the phrase %q is not in the file's phrases", name)
		}
		options := map[string]string{}
		for option, byLanguage := range phrase {
			if optionText, forEvery := byLanguage[""]; forEvery {
				options[option] = optionText
			} else if optionText, forThis := byLanguage[language]; forThis {
				options[option] = optionText
			}
		}
		if len(options) == 0 {
			return messageTemplate{}, fmt.Errorf("the phrase %q has no option in %s", name, language)
		}
		if template.options == nil {
			template.options = map[string]map[string]string{}
		}
		template.options[name] = options
	}
	return template, nil
}

// resolveTerms puts each `[[term]]` in text in its words for language, recording the terms it used.
//
// Mutates: used is the caller's record of the terms any language's text used, so a term in no text is refused
// once every language is read
func resolveTerms(
	text string,
	terms map[string]map[string]string,
	language string,
	used map[string]bool,
) (string, error) {
	names, placeholderNamesError := placeholderNames(text, "[[", "]]")
	if placeholderNamesError != nil {
		return "", placeholderNamesError
	}
	resolved := text
	for _, name := range names {
		words, defined := terms[name][language]
		if !defined {
			return "", fmt.Errorf("the term %q has no words for %s", name, language)
		}
		if strings.Contains(words, "[[") ||
			strings.Contains(words, "]]") ||
			strings.Contains(words, "<<") ||
			strings.Contains(words, ">>") {
			return "", fmt.Errorf("the term %q holds a term or a phrase, and nothing nests", name)
		}
		resolved = strings.ReplaceAll(resolved, "[["+name+"]]", words)
		used[name] = true
	}
	return resolved, nil
}

// placeholderNames returns the names between each open and close in text, sorted and once each,
// refusing an open with no close, a close with no open, and a name that is not camelCase.
func placeholderNames(text string, open string, close string) ([]string, error) {
	seen := map[string]bool{}
	rest := text
	for {
		start := strings.Index(rest, open)
		stray := strings.Index(rest, close)
		if start < 0 {
			if stray >= 0 {
				return nil, fmt.Errorf("%q with no %q before it", close, open)
			}
			break
		}
		if stray >= 0 && stray < start {
			return nil, fmt.Errorf("%q with no %q before it", close, open)
		}
		length := strings.Index(rest[start+len(open):], close)
		if length < 0 {
			return nil, fmt.Errorf("%q with no %q after it", open, close)
		}
		name := rest[start+len(open) : start+len(open)+length]
		if !isCamelCaseName(name) {
			return nil, fmt.Errorf("%s%s%s: a placeholder names a camelCase value or term", open, name, close)
		}
		seen[name] = true
		rest = rest[start+len(open)+length+len(close):]
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// isCamelCaseName is a lowercase letter followed by letters and digits.
func isCamelCaseName(text string) bool {
	if text == "" || text[0] < 'a' || text[0] > 'z' {
		return false
	}
	for _, character := range text {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// MessageOf is a rule's claim on one of its messages, taken in a package-level var so a missing entry
// panics when the package initializes and every test binary that links the rule fails at once.
func MessageOf(ruleName string, id string) MessageHandle {
	if _, found := currentMessages.Load().templates[ruleName][id]; !found {
		// Invariant: handles are taken in package-level vars, so a missing message fails every test binary
		// that links the rule when it initializes, and no build that passed its tests reaches here.
		panic(fmt.Sprintf("policy/messages: %s has no message %q", ruleName, id))
	}
	handle := MessageHandle{Rule: ruleName, Id: id}
	requestedMessages.Lock()
	requestedMessages.handles[handle] = true
	requestedMessages.Unlock()
	return handle
}

// UseMessages makes catalog the one handles render from until restore is called. It exists for the
// test that edits an entry and expects the edit in a rule's finding, which renders inside the rule and so
// cannot be handed a catalog; a test calling it must not run in parallel with others that render. A test
// of the catalog itself renders through MessageCatalog.Render instead and swaps nothing.
func UseMessages(catalog *MessageCatalog) (restore func()) {
	previous := currentMessages.Swap(catalog)
	return func() { currentMessages.Store(previous) }
}

// RequestedOptions lists every option a rule has claimed.
func RequestedOptions() []MessageOption {
	requestedMessages.Lock()
	defer requestedMessages.Unlock()
	options := make([]MessageOption, 0, len(requestedMessages.options))
	for option := range requestedMessages.options {
		options = append(options, option)
	}
	return options
}

// RequestedMessages lists every handle a rule has taken, sorted.
func RequestedMessages() []MessageHandle {
	requestedMessages.Lock()
	defer requestedMessages.Unlock()
	handles := make([]MessageHandle, 0, len(requestedMessages.handles))
	for handle := range requestedMessages.handles {
		handles = append(handles, handle)
	}
	sortMessageHandles(handles)
	return handles
}

func sortMessageHandles(handles []MessageHandle) {
	sort.Slice(handles, func(left, right int) bool {
		if handles[left].Rule != handles[right].Rule {
			return handles[left].Rule < handles[right].Rule
		}
		return handles[left].Id < handles[right].Id
	})
}
