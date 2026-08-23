package suppression

import "strings"

// parseDirective reads one comment's text and returns the directive it declares, or nil.
//
// The grammar, identical for every honored spelling:
//
//	<tool>-disable[-next-line|-line] [rule[, rule...]] [-- reason]
//
// Everything after the directive word is optional. No rules means blanket. No reason means the
// author did not say why, which is recorded rather than rejected — see Index.WithoutReason.
func parseDirective(commentText string) *Directive {
	body := strings.TrimSpace(stripCommentMarkers(commentText))

	rest, found := splitDirective(body)
	if !found {
		return nil
	}

	kind, rest, valid := splitScope(rest)
	if !valid {
		return nil
	}

	rules, reason := splitReason(rest)

	return &Directive{
		Kind:   kind,
		Rules:  parseRuleNames(rules),
		Reason: reason,
	}
}

// splitDirective finds an honored directive word at the start of the comment body.
//
// Anchored at the start on purpose. A directive has to be the first thing in its comment to count,
// or every sentence that mentions one becomes a suppression. This codebase contains exactly such
// sentences, including one inside a lint rule's own documentation that reads "opt out explicitly
// with an eslint-disable."
func splitDirective(body string) (rest string, found bool) {
	for _, candidate := range disableDirectives {
		if strings.HasPrefix(body, candidate) {
			return body[len(candidate):], true
		}
	}
	return "", false
}

// splitScope reads the scope suffix and returns what follows it.
//
// The order of these cases is load-bearing and is the highest-consequence bug available in this
// file. `-next-line` must be tested before the bare form, or all 372 next-line directives in the
// corpus read as file-level disables — each one silencing every rule for the remainder of its file,
// with nothing in the output saying so. Go's switch does not fall through, but the cases are still
// written longest-first so the ordering is visible to a reader rather than incidental.
func splitScope(rest string) (kind Kind, remainder string, valid bool) {
	switch {
	case strings.HasPrefix(rest, "-next-line"):
		return KindNextLine, rest[len("-next-line"):], true
	case strings.HasPrefix(rest, "-line"):
		return KindSameLine, rest[len("-line"):], true
	case rest == "" || rest[0] == ' ' || rest[0] == '\t':
		// The bare, file-level form.
		return KindFile, rest, true
	}

	// The directive is a prefix of a longer word — `eslint-disable-nonsense`, or `eslint-disabled`.
	// Prose that merely contains a directive word must not silence a rule.
	return KindFile, "", false
}

// splitReason separates the rule list from the ` -- reason` that may follow it.
//
// The separator is `--`, which is the convention the existing corpus uses and ESLint's own. A rule
// name cannot contain `--`, so the first occurrence wins.
func splitReason(rest string) (rules string, reason string) {
	if index := strings.Index(rest, "--"); index >= 0 {
		return strings.TrimSpace(rest[:index]), strings.TrimSpace(rest[index+2:])
	}
	return strings.TrimSpace(rest), ""
}

// parseRuleNames splits a comma-separated rule list, dropping empties.
//
// The corpus writes multi-rule lines: `nexus/consistency-require-type-suffix,
// nexus/consistency-no-abbreviated-identifier` on one comment. Splitting on comma rather than
// whitespace matters because that is what the corpus and ESLint both do — a whitespace split reads
// two rules as one unknown name, which matches nothing and silences nothing while looking like it
// silenced something.
func parseRuleNames(list string) []string {
	if list == "" {
		return nil
	}
	names := []string{}
	for _, part := range strings.Split(list, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// stripCommentMarkers removes `//`, `/*`, `*/`, and any `*` continuation markers.
//
// A block comment spanning lines carries `*` at the start of each continuation line, and a
// suppression's reason often wraps. Joining those into one line before parsing is what lets a
// wrapped reason parse as a reason rather than as a truncated one. JSX `{/* ... */}` arrives here
// already unwrapped by the scanner, which returns the comment span rather than the braces.
func stripCommentMarkers(commentText string) string {
	text := commentText

	if strings.HasPrefix(text, "//") {
		return text[2:]
	}

	text = strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
	if !strings.Contains(text, "\n") {
		return text
	}

	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = strings.TrimPrefix(strings.TrimSpace(line), "*")
	}
	return strings.Join(lines, " ")
}
