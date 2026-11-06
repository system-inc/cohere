package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// MessageCatalog is every message file, read and checked, with each rule's text resolved for its
// language.
type MessageCatalog struct {
	// templates holds each rule's messages by id.
	templates map[string]map[string]messageTemplate
	// languages holds each rule's language.
	languages map[string]string
}

// add reads one file into the catalog.
func (catalog *MessageCatalog) add(idea string, data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file messageFile
	if decodeError := decoder.Decode(&file); decodeError != nil {
		return decodeError
	}
	if len(file.Rules) == 0 {
		return fmt.Errorf("names no rule, so nothing renders its messages")
	}
	if len(file.Messages) == 0 {
		return fmt.Errorf("holds no message")
	}
	for language, ruleName := range file.Rules {
		if !messageLanguages[language] {
			return fmt.Errorf(
				"rules: %q is not %q, %q or %q",
				language,
				MessageLanguageTypeScript,
				MessageLanguageSwift,
				MessageLanguageGo,
			)
		}
		if _, leaf, found := strings.Cut(ruleName, "/"); !found || leaf == "" {
			return fmt.Errorf("rules: %q is not a namespaced rule name", ruleName)
		}
		if _, taken := catalog.languages[ruleName]; taken {
			return fmt.Errorf("rules: %q is named by another message file too", ruleName)
		}
	}
	// The file is named for its idea: the first rule messageLanguagesNamingTheFile finds, after its
	// namespace.
	namedFor := ""
	for _, language := range messageLanguagesNamingTheFile {
		if ruleName := file.Rules[language]; ruleName != "" {
			namedFor = ruleName
			break
		}
	}
	if _, leaf, _ := strings.Cut(namedFor, "/"); leaf != idea {
		return fmt.Errorf("is named %q, and its idea is %q", idea, leaf)
	}

	stringPhrases, objectPhrases, readPhrasesError := readPhrases(file.Phrases, file.Rules)
	if readPhrasesError != nil {
		return readPhrasesError
	}
	// The messages naming each phrase, so a phrase no message names, and a string phrase only one
	// message names, are refused once every message is read.
	phraseUsers := map[string]map[string]bool{}

	for id, message := range file.Messages {
		if !isCamelCaseName(id) {
			return fmt.Errorf("message %q: an id is a camelCase noun phrase", id)
		}
		if strings.TrimSpace(message.Text) == "" {
			return fmt.Errorf("message %q has no text", id)
		}
		languages, messageLanguagesOfError := messageLanguagesOf(message, file.Rules)
		if messageLanguagesOfError != nil {
			return fmt.Errorf("message %q: %w", id, messageLanguagesOfError)
		}
		if len(message.Terms) > 0 && len(languages) == 1 {
			return fmt.Errorf("message %q: one language has nothing to vary, so it carries no terms", id)
		}
		usedTerms := map[string]bool{}
		for _, language := range languages {
			ruleName := file.Rules[language]
			text, resolveTermsError := resolveTerms(message.Text, message.Terms, language, usedTerms)
			if resolveTermsError != nil {
				return fmt.Errorf("message %q, %s: %w", id, language, resolveTermsError)
			}
			template, resolvePhrasesError := resolvePhrases(text, stringPhrases, objectPhrases, language)
			if resolvePhrasesError != nil {
				return fmt.Errorf("message %q, %s: %w", id, language, resolvePhrasesError)
			}
			if _, placeholderNamesError := placeholderNames(template.text, "{{", "}}"); placeholderNamesError != nil {
				return fmt.Errorf("message %q, %s: %w", id, language, placeholderNamesError)
			}
			// Ignored: resolvePhrases read this same text's phrases above and returned the error this could give.
			names, _ := placeholderNames(text, "<<", ">>")
			for _, name := range names {
				if phraseUsers[name] == nil {
					phraseUsers[name] = map[string]bool{}
				}
				phraseUsers[name][id] = true
			}
			if catalog.templates[ruleName] == nil {
				catalog.templates[ruleName] = map[string]messageTemplate{}
			}
			catalog.templates[ruleName][id] = template
		}
		for term, byLanguage := range message.Terms {
			if !usedTerms[term] {
				return fmt.Errorf("message %q: the term %q is in no text", id, term)
			}
			for language := range byLanguage {
				if !slices.Contains(languages, language) {
					return fmt.Errorf(
						"message %q: the term %q gives %s, which the message is not rendered in",
						id,
						term,
						language,
					)
				}
			}
		}
	}
	for name := range stringPhrases {
		if len(phraseUsers[name]) < 2 {
			return fmt.Errorf(
				"the string phrase %q is in %d message, and one shared by fewer than two stays inline",
				name,
				len(phraseUsers[name]),
			)
		}
	}
	for name := range objectPhrases {
		if len(phraseUsers[name]) == 0 {
			return fmt.Errorf("the phrase %q is in no message", name)
		}
	}
	for language, ruleName := range file.Rules {
		catalog.languages[ruleName] = language
	}
	return nil
}

// Option is handle.Option against this catalog rather than the current one, so a test can claim an option
// of a catalog it built without swapping the one every other test renders from.
func (catalog *MessageCatalog) Option(handle MessageHandle, phrase string, name string) MessageOption {
	template, found := catalog.templates[handle.Rule][handle.Id]
	if _, held := template.options[phrase][name]; !found || !held {
		// Invariant: options are claimed in package-level vars like handles, so a missing option fails every
		// test binary that links the rule when it initializes, and no build that passed its tests reaches here.
		panic(fmt.Sprintf(
			"policy/messages: %s %q has no option %q of the phrase %q",
			handle.Rule,
			handle.Id,
			name,
			phrase,
		))
	}
	option := MessageOption{Rule: handle.Rule, Id: handle.Id, Phrase: phrase, Name: name}
	requestedMessages.Lock()
	requestedMessages.options[option] = true
	requestedMessages.Unlock()
	return option
}

// Render is handle.Render against this catalog rather than the current one. A test of the catalog itself
// renders through this and swaps nothing, so it runs in parallel with every test that renders the real
// messages: swapping the global under t.Parallel was a race, and one test read another's catalog
// (#hzxspf5).
func (catalog *MessageCatalog) Render(handle MessageHandle, values map[string]string, options ...MessageOption) string {
	template, found := catalog.templates[handle.Rule][handle.Id]
	if !found {
		// Invariant: a rule's handle comes from MessageOf, which checked that the message exists, and a test
		// rendering through a catalog it built names a message it wrote.
		panic(fmt.Sprintf("policy/messages: %s has no message %q", handle.Rule, handle.Id))
	}
	text := template.text
	chosen := map[string]bool{}
	for _, option := range options {
		optionText, held := template.options[option.Phrase][option.Name]
		if option.Rule != handle.Rule || option.Id != handle.Id || !held || chosen[option.Phrase] {
			// Invariant: a rule passes the options it claimed for this message, so the rule's own tests, which
			// render every message it reports, fail here before a wrong option can ship.
			panic(fmt.Sprintf(
				"policy/messages: %s %q was given the option %q of %q, which it does not take",
				handle.Rule,
				handle.Id,
				option.Name,
				option.Phrase,
			))
		}
		chosen[option.Phrase] = true
		text = strings.ReplaceAll(text, "<<"+option.Phrase+">>", optionText)
	}
	if len(chosen) != len(template.options) {
		// Invariant: a rule picks one option per phrase its message uses, which its own tests render.
		panic(fmt.Sprintf(
			"policy/messages: %s %q picks from %d phrases, and was given %d options",
			handle.Rule,
			handle.Id,
			len(template.options),
			len(chosen),
		))
	}
	// Ignored: the template's text and every option's were read as well-formed when the catalog loaded, and
	// putting a whole option into a whole slot makes no new placeholder.
	needed, _ := placeholderNames(text, "{{", "}}")
	if len(values) != len(needed) {
		// Invariant: a rule gives the values its message's text uses, which its own tests render.
		panic(fmt.Sprintf(
			"policy/messages: %s %q uses the values %v, and was given %d",
			handle.Rule,
			handle.Id,
			needed,
			len(values),
		))
	}
	for _, name := range needed {
		value, given := values[name]
		if !given {
			// Invariant: a rule gives the values its message's text uses, which its own tests render.
			panic(fmt.Sprintf(
				"policy/messages: %s %q uses the value %q, and was not given it",
				handle.Rule,
				handle.Id,
				name,
			))
		}
		text = strings.ReplaceAll(text, "{{"+name+"}}", value)
	}
	return text
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

// OptionsFor lists every object-phrase option the catalog holds for rules of one language, sorted.
func (catalog *MessageCatalog) OptionsFor(language string) []MessageOption {
	var options []MessageOption
	for ruleName, templates := range catalog.templates {
		if catalog.languages[ruleName] != language {
			continue
		}
		for id, template := range templates {
			for phrase, byName := range template.options {
				for name := range byName {
					options = append(options, MessageOption{Rule: ruleName, Id: id, Phrase: phrase, Name: name})
				}
			}
		}
	}
	sort.Slice(options, func(left, right int) bool {
		return fmt.Sprint(options[left]) < fmt.Sprint(options[right])
	})
	return options
}
