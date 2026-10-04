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
//
//go:embed messages/*.json
var messageFiles embed.FS

// The languages a message file speaks for, as its `rules` and its terms spell them.
const (
	MessageLanguageTypeScript = "TypeScript"
	MessageLanguageSwift      = "Swift"
)

// messageLanguages are the languages a file may name.
var messageLanguages = map[string]bool{MessageLanguageTypeScript: true, MessageLanguageSwift: true}

// messageFile is one file under messages/, as written.
type messageFile struct {
	Rules    map[string]string             `json:"rules"`
	Messages map[string]messageFileMessage `json:"messages"`
}

type messageFileMessage struct {
	Text  string                       `json:"text"`
	Terms map[string]map[string]string `json:"terms"`
}

// MessageCatalog is every message file, read and checked, with each rule's text resolved for its
// language.
type MessageCatalog struct {
	// templates holds each rule's messages by id.
	templates map[string]map[string]messageTemplate
	// languages holds each rule's language.
	languages map[string]string
}

// messageTemplate is one message for one rule, its terms resolved, and the values its text uses.
type messageTemplate struct {
	text   string
	values []string
}

// MessageHandle is a rule's claim on one of its messages, taken when the rule's package initializes.
type MessageHandle struct {
	Rule string
	Id   string
}

// currentMessages is the catalog handles render from: the embedded files, unless a test swapped in an
// edited copy through UseMessages.
var currentMessages atomic.Pointer[MessageCatalog]

// requestedMessages records every handle taken, so a test can fail on an entry no rule renders.
var requestedMessages = struct {
	sync.Mutex
	handles map[MessageHandle]bool
}{handles: map[MessageHandle]bool{}}

// Messages is the embedded catalog, read once. An invalid file panics at startup: a message read
// wrongly would print the wrong words in every finding while every check reported the file clean.
var Messages = mustLoadEmbeddedMessages()

// MessageFiles is the embedded messages directory, its files at the root, for a test that edits one.
func MessageFiles() fs.FS {
	files, err := fs.Sub(messageFiles, "messages")
	if err != nil {
		panic(fmt.Sprintf("policy/messages: %v", err))
	}
	return files
}

func mustLoadEmbeddedMessages() *MessageCatalog {
	catalog, err := LoadMessages(MessageFiles())
	if err != nil {
		panic(fmt.Sprintf("policy/messages: %v", err))
	}
	currentMessages.Store(catalog)
	return catalog
}

// LoadMessages reads every `.json` file at the root of files as a message file and checks it. It
// refuses an unknown key, a file named for something other than its idea, a rule named by two files,
// an id that is not camelCase, a placeholder that is malformed or names nothing, a term missing a
// language or given one the file does not speak for, a term no text uses, and a term on a lone rule.
func LoadMessages(files fs.FS) (*MessageCatalog, error) {
	names, err := fs.Glob(files, "*.json")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no message files, so no house rule could render a finding")
	}
	catalog := &MessageCatalog{
		templates: map[string]map[string]messageTemplate{},
		languages: map[string]string{},
	}
	for _, name := range names {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		if err := catalog.add(strings.TrimSuffix(path.Base(name), ".json"), data); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	return catalog, nil
}

// add reads one file into the catalog.
func (catalog *MessageCatalog) add(idea string, data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file messageFile
	if err := decoder.Decode(&file); err != nil {
		return err
	}
	if len(file.Rules) == 0 {
		return fmt.Errorf("names no rule, so nothing renders its messages")
	}
	if len(file.Messages) == 0 {
		return fmt.Errorf("holds no message")
	}
	for language, ruleName := range file.Rules {
		if !messageLanguages[language] {
			return fmt.Errorf("rules: %q is not %q or %q", language, MessageLanguageTypeScript, MessageLanguageSwift)
		}
		if _, leaf, found := strings.Cut(ruleName, "/"); !found || leaf == "" {
			return fmt.Errorf("rules: %q is not a namespaced rule name", ruleName)
		}
		if _, taken := catalog.languages[ruleName]; taken {
			return fmt.Errorf("rules: %q is named by another message file too", ruleName)
		}
	}
	// The file is named for its idea: the TypeScript rule's name after its namespace, the original, or
	// the Swift rule's when there is no TypeScript one.
	namedFor := file.Rules[MessageLanguageTypeScript]
	if namedFor == "" {
		namedFor = file.Rules[MessageLanguageSwift]
	}
	if _, leaf, _ := strings.Cut(namedFor, "/"); leaf != idea {
		return fmt.Errorf("is named %q, and its idea is %q", idea, leaf)
	}

	for id, message := range file.Messages {
		if !isCamelCaseName(id) {
			return fmt.Errorf("message %q: an id is a camelCase noun phrase", id)
		}
		if strings.TrimSpace(message.Text) == "" {
			return fmt.Errorf("message %q has no text", id)
		}
		if len(message.Terms) > 0 && len(file.Rules) == 1 {
			return fmt.Errorf("message %q: a lone rule has nothing to vary, so it carries no terms", id)
		}
		usedTerms := map[string]bool{}
		for language, ruleName := range file.Rules {
			text, err := resolveTerms(message.Text, message.Terms, language, usedTerms)
			if err != nil {
				return fmt.Errorf("message %q, %s: %w", id, language, err)
			}
			values, err := placeholderNames(text, "{{", "}}")
			if err != nil {
				return fmt.Errorf("message %q, %s: %w", id, language, err)
			}
			if catalog.templates[ruleName] == nil {
				catalog.templates[ruleName] = map[string]messageTemplate{}
			}
			catalog.templates[ruleName][id] = messageTemplate{text: text, values: values}
		}
		for term, byLanguage := range message.Terms {
			if !usedTerms[term] {
				return fmt.Errorf("message %q: the term %q is in no text", id, term)
			}
			for language := range byLanguage {
				if _, spoken := file.Rules[language]; !spoken {
					return fmt.Errorf("message %q: the term %q gives %s, which the file names no rule for", id, term, language)
				}
			}
		}
	}
	for language, ruleName := range file.Rules {
		catalog.languages[ruleName] = language
	}
	return nil
}

// resolveTerms puts each `[[term]]` in text in its words for language, recording the terms it used.
func resolveTerms(text string, terms map[string]map[string]string, language string, used map[string]bool) (string, error) {
	names, err := placeholderNames(text, "[[", "]]")
	if err != nil {
		return "", err
	}
	for _, name := range names {
		words, defined := terms[name][language]
		if !defined {
			return "", fmt.Errorf("the term %q has no words for %s", name, language)
		}
		if strings.Contains(words, "[[") || strings.Contains(words, "]]") {
			return "", fmt.Errorf("the term %q holds a term, and terms do not nest", name)
		}
		text = strings.ReplaceAll(text, "[["+name+"]]", words)
		used[name] = true
	}
	return text, nil
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
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// MessageOf is a rule's claim on one of its messages, taken in a package-level var so a missing entry
// panics when the package initializes and every test binary that links the rule fails at once.
func MessageOf(ruleName string, id string) MessageHandle {
	if _, found := currentMessages.Load().templates[ruleName][id]; !found {
		panic(fmt.Sprintf("policy/messages: %s has no message %q", ruleName, id))
	}
	handle := MessageHandle{Rule: ruleName, Id: id}
	requestedMessages.Lock()
	requestedMessages.handles[handle] = true
	requestedMessages.Unlock()
	return handle
}

// Render is the message's text for its rule's language with values put in. It reads the current
// catalog, so a test that swapped in an edited copy sees the edit. A value the text does not use, or
// one it uses and was not given, panics: a finding reading `{{constructor}}` is the silent version.
func (handle MessageHandle) Render(values map[string]string) string {
	template, found := currentMessages.Load().templates[handle.Rule][handle.Id]
	if !found {
		panic(fmt.Sprintf("policy/messages: %s has no message %q", handle.Rule, handle.Id))
	}
	if len(values) != len(template.values) {
		panic(fmt.Sprintf("policy/messages: %s %q uses the values %v, and was given %d", handle.Rule, handle.Id, template.values, len(values)))
	}
	text := template.text
	for _, name := range template.values {
		value, given := values[name]
		if !given {
			panic(fmt.Sprintf("policy/messages: %s %q uses the value %q, and was not given it", handle.Rule, handle.Id, name))
		}
		text = strings.ReplaceAll(text, "{{"+name+"}}", value)
	}
	return text
}

// UseMessages makes catalog the one handles render from until restore is called. It exists for the
// test that edits an entry and expects the edit in a rule's finding; a test calling it must not run in
// parallel with others that render.
func UseMessages(catalog *MessageCatalog) (restore func()) {
	previous := currentMessages.Swap(catalog)
	return func() { currentMessages.Store(previous) }
}

// MessagesFor lists every message the catalog holds for rules of one language, sorted.
func (catalog *MessageCatalog) MessagesFor(language string) []MessageHandle {
	var handles []MessageHandle
	for ruleName, templates := range catalog.templates {
		if catalog.languages[ruleName] != language {
			continue
		}
		for id := range templates {
			handles = append(handles, MessageHandle{Rule: ruleName, Id: id})
		}
	}
	sortMessageHandles(handles)
	return handles
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
