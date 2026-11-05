package css

// src/language-css/parser-postcss.js, the css and scss parsers: parseCss, parseScss, parseWithParser and
// parseNestedCSS.
//
// The parser is "css" or "scss". Less is not ported: where upstream branches on options.parser === "less"
// the branch is left out with a note, and parseLess (postcss-less, replaceQuotesInInlineComments) is not
// here.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/system-inc/cohere/internal/format/css/postcss"
	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// parseOptions is the part of Prettier's options object the parser reads and writes.
type parseOptions struct {
	// parser is options.parser, "css" or "scss".
	parser string
	// originalText is options.originalText, which parseWithParser sets.
	originalText string
}

// javaScriptError is a throw upstream does not catch until parseCss's caller: a TypeError from reading
// a property of undefined, or an Error the glue raises. parseCss turns it back into an error.
type javaScriptError struct {
	message string
}

// parse is the css parser's parse: the css-root the printer receives, with node.source.startOffset and
// endOffset (and each node's Range) computed by loc.js. Comments are css-comment nodes in the tree; the
// css printer has no separate comments list.
//
// text is what Prettier's core hands the parser: a byte order mark removed and line endings normalized
// to \n. A postcss CssSyntaxError comes back marked printing.Syntax (by postcss.Parse); anything else
// upstream would have thrown (a TypeError on a custom property it cannot read, say) is a plain error.
func parse(text string) (*estree.Node, error) {
	return parseCss(text, &parseOptions{parser: "css"})
}

// parseSCSS is the scss parser's parse, the same as parse but through postcss-scss
// (postcss.ParseSCSS), with options.parser "scss" for the glue's scss branches.
func parseSCSS(text string) (*estree.Node, error) {
	return parseScss(text, &parseOptions{parser: "scss"})
}

// parseWithParserName is the parse of the parser named: "css" or "scss".
func parseWithParserName(text string, parser string) (*estree.Node, error) {
	if parser == "scss" {
		return parseSCSS(text)
	}
	return parse(text)
}

// replaceNonLineBreaksWithSpace is src/utilities/replace-non-line-breaks-with-space.js in bytes: every
// byte but \n becomes a space, so every byte offset after it is unchanged. Upstream keeps the UTF-16
// length; the port's offsets are bytes. (A postcss column on the same line after a blanked non-ASCII
// character counts the spaces, so it can differ from upstream's there; Prettier reads only offsets and
// lines.)
func replaceNonLineBreaksWithSpace(text string) string {
	replaced := []byte(text)
	for index, character := range replaced {
		if character != '\n' {
			replaced[index] = ' '
		}
	}
	return string(replaced)
}

var (
	defaultScssDirective = "!default"
	globalScssDirective  = "!global"
)

// matchScssDirective is value.match(/(\s*)(!default).*$/) (or !global): the index where the match
// starts and the matched text, which runs to the end of the value. The directive matches only where no
// line terminator follows it, since `.` stops at one and `$` is the end of the input.
func matchScssDirective(value string, directive string) (int, string, bool) {
	for index := 0; index <= len(value); index++ {
		if index > 0 && index < len(value) && value[index]&0xC0 == 0x80 {
			continue
		}
		afterWhitespace := len(value) - len(trimStart(value[index:]))
		if !strings.HasPrefix(value[afterWhitespace:], directive) {
			continue
		}
		if strings.ContainsAny(value[afterWhitespace+len(directive):], "\n\r\xe2\x80\xa8\xe2\x80\xa9") {
			continue
		}
		return index, value[index:], true
	}
	return 0, "", false
}

// customSelectorName is node.params.match(/:--\S+\s+/)[0].trim(): the first `:--` followed by
// non-whitespace and then whitespace, without the whitespace. Upstream throws a TypeError on null when
// there is none.
func customSelectorName(parameters string) string {
	for from := 0; ; {
		index := indexFrom(parameters, ":--", from)
		if index == -1 {
			// Invariant: this is upstream's TypeError, not a crash: parseCatching recovers every
			// javaScriptError into the parse's error, as upstream's caller catches it.
			panic(javaScriptError{message: "Cannot read properties of null (reading '0')"})
		}
		rest := parameters[index+3:]
		nameEnd := strings.IndexFunc(rest, ecmascripttext.IsWhitespace)
		if nameEnd > 0 {
			return parameters[index : index+3+nameEnd]
		}
		from = index + 1
	}
}

// javaScriptNonWhitespaceClass is JavaScript's \S.
var javaScriptNonWhitespaceClass = "[^" + javaScriptWhitespaceClass[1:]

var (
	atRootWithPattern = regexp.MustCompile(
		`(?s)^\(` + javaScriptWhitespaceClass + `*(?:without|with)` + javaScriptWhitespaceClass + `*:.+\)$`,
	)
	// /(\$\S+?)(\s+)?\.{3}/
	scssVariableArgumentsPattern = regexp.MustCompile(
		`(\$` + javaScriptNonWhitespaceClass + `+?)(` + javaScriptWhitespaceClass + `+)?\.{3}`,
	)
	// /^(?!if)([^"'\s(]+)(\s+)\(/, the lookahead tested apart since RE2 has none.
	scssDirectiveFunctionPattern = regexp.MustCompile(`^([^"'(` +
		javaScriptWhitespaceClass[1:len(javaScriptWhitespaceClass)-1] +
		`]+)(` +
		javaScriptWhitespaceClass +
		`+)\(`)
)

var valueParamsAtRuleNames = map[string]bool{
	"namespace":    true,
	"supports":     true,
	"if":           true,
	"else":         true,
	"for":          true,
	"each":         true,
	"while":        true,
	"debug":        true,
	"mixin":        true,
	"include":      true,
	"function":     true,
	"return":       true,
	"define-mixin": true,
	"add-mixin":    true,
}

// parseNestedCSS returns its node, or the node upstream returns in its place. Upstream's recursion
// ignores what it returns, so only parseWithParser's call can see a replacement.
func parseNestedCSS(node *estree.Node, options *parseOptions) *estree.Node {
	if node == nil {
		return node
	}

	for _, key := range node.Keys() {
		switch child := node.Get(key).(type) {
		case *estree.Node:
			parseNestedCSS(child, options)
		case []*estree.Node:
			for _, element := range child {
				parseNestedCSS(element, options)
			}
		}
	}

	if !hasStringType(node) {
		return node
	}

	/* c8 ignore next */
	raws, _ := node.Get("raws").(map[string]any)
	if raws == nil {
		raws = map[string]any{}
		node.Set("raws", raws)
	}

	// Custom properties looks like declarations
	if property, isString := node.Get(
		"prop",
	).(string); node.Type() == "css-decl" && isString && strings.HasPrefix(property, "--") {
		if value, isString := node.Get("value").(string); isString && strings.HasPrefix(value, "{") {
			var rules []*estree.Node
			hasRules := false
			if strings.HasSuffix(trimEnd(value), "}") {
				source := sourceOf(node)
				startOffset, _ := numberIn(mapIn(source, "start"), "offset")
				endOffset, _ := numberIn(mapIn(source, "end"), "offset")
				textBefore := sliceJavaScript(options.originalText, 0, startOffset)
				nodeText := strings.Repeat("a", len(property)) +
					sliceJavaScript(options.originalText, startOffset+len(property), endOffset)
				fakeContent := replaceNonLineBreaksWithSpace(textBefore) + nodeText
				// The less parser (options.parser === "less") is not ported.
				parse := parseCss
				if options.parser == "scss" {
					parse = parseScss
				}
				nestedOptions := *options
				ast, parseError := parse(fakeContent, &nestedOptions)
				if parseError != nil {
					// noop
					ast = nil
				}
				if nodes := ast.List("nodes"); len(nodes) == 1 && nodes[0].Is("css-rule") {
					rules = nodes[0].List("nodes")
					hasRules = true
				}
			}
			if hasRules {
				node.Set("value", estree.New("css-rule", 0, 0, "nodes", rules))
			} else {
				rawValue, isObject := raws["value"].(map[string]any)
				if !isObject {
					// Invariant: this is upstream's TypeError, not a crash: parseCatching recovers every
					// javaScriptError into the parse's error, as upstream's caller catches it.
					panic(javaScriptError{message: "Cannot read properties of undefined (reading 'raw')"})
				}
				node.Set("value", estree.New("value-unknown", 0, 0, "value", rawValue["raw"]))
			}
			return node
		}
	}

	selector := ""

	if nodeSelector, isString := node.Get("selector").(string); isString {
		if rawSelector, isObject := scssOrRaw(node, "selector"); isObject {
			selector = rawSelector
		} else {
			selector = nodeSelector
		}

		if between, isString := raws["between"].(string); isString && trim(between) != "" {
			selector += between
		}

		raws["selector"] = selector
	}

	value := ""

	if nodeValue, isString := node.Get("value").(string); isString {
		if rawValue, isObject := scssOrRaw(node, "value"); isObject {
			value = rawValue
		} else {
			value = nodeValue
		}

		raws["value"] = trim(value)
	}

	parameters := ""

	if nodeParameters, isString := node.Get("params").(string); isString {
		if rawParameters, isObject := scssOrRaw(node, "params"); isObject {
			parameters = rawParameters
		} else {
			parameters = nodeParameters
		}

		if afterName, isString := raws["afterName"].(string); isString && trim(afterName) != "" {
			parameters = afterName + parameters
		}

		if between, isString := raws["between"].(string); isString && trim(between) != "" {
			parameters += between
		}

		parameters = trim(parameters)

		raws["params"] = parameters
	}

	// Ignore less mixin declaration
	if trim(selector) != "" {
		// Upstream: `TODO: confirm this code is dead`
		/* c8 ignore next 3 */
		if strings.HasPrefix(selector, "@") && strings.HasSuffix(selector, ":") {
			return node
		}

		// The Less mixin case (node.mixin, which only postcss-less sets) is not ported.

		// Check on SCSS nested property
		if isScssNestedPropertyNode(node, options) {
			node.Set("isScssNestedProperty", true)
		}

		node.Set("selector", parseSelector(selector))

		return node
	}

	if trim(value) != "" {
		if index, matched, found := matchScssDirective(value, defaultScssDirective); found {
			value = value[:index]
			node.Set("scssDefault", true)

			if trim(matched) != "!default" {
				raws["scssDefault"] = matched
			}
		}

		if index, matched, found := matchScssDirective(value, globalScssDirective); found {
			value = value[:index]
			node.Set("scssGlobal", true)

			if trim(matched) != "!global" {
				raws["scssGlobal"] = matched
			}
		}

		if strings.HasPrefix(value, "progid:") {
			return estree.New("value-unknown", 0, 0, "value", value)
		}

		node.Set("value", parseValue(value, options))
	}

	// The Less merge (`+:`) and `extend(` cases (options.parser === "less") are not ported.

	if node.Type() == "css-atrule" {
		// The Less mixin and function cases are not ported.

		// only CSS support custom-selector
		if options.parser == "css" && node.String("name") == "custom-selector" {
			nodeParameters, _ := node.Get("params").(string)
			customSelector := trim(customSelectorName(nodeParameters))
			node.Set("customSelector", customSelector)
			node.Set(
				"selector",
				parseSelector(trim(sliceJavaScript(nodeParameters, len(customSelector), len(nodeParameters)))),
			)
			node.Delete("params")
			return node
		}

		// The Less variable cases (`@color:blue;`, `@color :blue;`) are not ported.
	}

	if node.Type() == "css-atrule" && parameters != "" {
		name := node.String("name")

		if name == "warn" || name == "error" {
			node.Set("params", estree.New("media-unknown", 0, 0, "value", parameters))

			return node
		}

		if name == "extend" || name == "nest" {
			node.Set("selector", parseSelector(parameters))
			node.Delete("params")

			return node
		}

		if name == "at-root" {
			if atRootWithPattern.MatchString(parameters) {
				node.Set("params", parseValue(parameters, options))
			} else {
				node.Set("selector", parseSelector(parameters))
				node.Delete("params")
			}

			return node
		}

		lowercasedName := toLowerCase(name)
		if isModuleRuleName(lowercasedName) {
			node.Set("import", true)
			node.Delete("filename")
			node.Set("params", parseValue(parameters, options))
			return node
		}

		if valueParamsAtRuleNames[name] {
			// Remove unnecessary spaces in SCSS variable arguments
			// Move spaces after the `...`, so we can keep the range correct
			if match := scssVariableArgumentsPattern.FindStringSubmatchIndex(parameters); match != nil {
				spaces := ""
				if match[4] != -1 {
					spaces = parameters[match[4]:match[5]]
				}
				parameters = parameters[:match[0]] +
					parameters[match[2]:match[3]] +
					"..." +
					spaces +
					parameters[match[1]:]
			}
			// Remove unnecessary spaces before SCSS control, mixin and function directives
			// Move spaces after the `(`, so we can keep the range correct
			// Only match the first function call at the beginning, not nested ones
			if !strings.HasPrefix(parameters, "if") {
				parameters = replaceFirst(scssDirectiveFunctionPattern, parameters, "$1($2")
			}

			node.Set("value", parseValue(parameters, options))
			node.Delete("params")

			return node
		}

		if lowercasedName == "media" || lowercasedName == "custom-media" {
			if strings.Contains(parameters, "#{") {
				// Workaround for media at rule with scss interpolation
				return estree.New("media-unknown", 0, 0, "value", parameters)
			}

			node.Set("params", parseMediaQuery(parameters))

			return node
		}

		node.Set("params", parameters)

		return node
	}

	return node
}

// scssOrRaw is `raws[key].scss ?? raws[key].raw`, read from node.raws, and whether raws[key] is an
// object at all.
func scssOrRaw(node *estree.Node, key string) (string, bool) {
	raws, _ := node.Get("raws").(map[string]any)
	raw, isObject := raws[key].(map[string]any)
	if !isObject {
		return "", false
	}
	if scss, isPresent := raw["scss"]; isPresent && scss != nil {
		text, _ := scss.(string)
		return text, true
	}
	text, _ := raw["raw"].(string)
	return text, true
}

// parseWithParser is upstream's parseWithParser: the text parsed, its front matter set aside, then the
// glue's walks over the tree.
//
// Mutates: options gets the text as its originalText, as upstream sets options.originalText, so
// parseNestedCSS can slice a custom property block out of it.
func parseWithParser(
	parse func(string) (*estree.Node, error),
	text string,
	options *parseOptions,
) (*estree.Node, error) {
	frontMatter, textToParse := parseFrontMatter(text)

	// Prevent file access
	// https://github.com/postcss/postcss/blob/4f4e2932fc97e2c117e1a4b15f0272ed551ed59d/lib/previous-map.js#L18
	result, parseError := parse(textToParse)
	if parseError != nil {
		// Upstream rethrows a CssSyntaxError as createError(`${name}: ${reason}`, {loc}); postcss.Parse
		// and postcss.ParseSCSS have already marked it printing.Syntax, with its name, reason, line and
		// column.
		return nil, parseError
	}

	options.originalText = text
	addTypePrefix(result, "css-", nil)
	result = parseNestedCSS(result, options)

	calculateLoc(result, text)

	if frontMatter != nil {
		result.Set("frontMatter", frontMatter)
	}

	return result, nil
}

// parseCss is upstream's parseCss: parseWithParser over postcss's parse.
func parseCss(text string, options *parseOptions) (*estree.Node, error) {
	return parseCatching(postcss.Parse, text, options)
}

// The less parser (parseLess, postcss-less behind replaceQuotesInInlineComments) is not ported.

// parseScss is upstream's parseScss: parseWithParser over postcss-scss's parse.
func parseScss(text string, options *parseOptions) (*estree.Node, error) {
	return parseCatching(postcss.ParseSCSS, text, options)
}

// parseCatching is parseWithParser, and the one place a throw upstream would let through comes back as
// an error.
func parseCatching(
	parse func(string) (*estree.Node, error),
	text string,
	options *parseOptions,
) (root *estree.Node, parseCatchingError error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			root = nil
			if thrown, isThrown := recovered.(javaScriptError); isThrown {
				parseCatchingError = errors.New(thrown.message)
				return
			}
			parseCatchingError = fmt.Errorf("css parse: %v", recovered)
		}
	}()

	root, parseCatchingError = parseWithParser(parse, text, options)
	if parseCatchingError != nil {
		return nil, parseCatchingError
	}
	setRanges(root)
	return root, nil
}

// parseFrontMatter is src/main/front-matter/parse.js, through markdown's port of it (getFrontMatter),
// returning the front matter as the node parseWithParser puts on the root:
// { ...frontMatter, type: "front-matter", source: { startOffset, endOffset } }. The content is the text
// with the front matter blanked byte for byte, so offsets after it are unchanged.
func parseFrontMatter(text string) (*estree.Node, string) {
	frontMatter, _ := mdast.ParseFrontMatter(text)
	if frontMatter == nil {
		return nil, text
	}

	raw := frontMatter.Raw
	lines := strings.Split(raw, "\n")
	// explicitLanguage is null until the front matter names one: written first, so the key keeps its
	// place, and Set below replaces the value in place.
	node := estree.New("front-matter", 0, len(raw),
		"language", frontMatter.Language,
		"explicitLanguage", nil,
		"value", frontMatter.Value,
		"startDelimiter", frontMatter.StartDelimiter,
		"endDelimiter", frontMatter.EndDelimiter,
		"raw", raw,
		"start", map[string]any{"line": 1, "column": 0, "index": 0},
		// line and column are getters upstream; column counts UTF-16 units.
		"end", map[string]any{
			"index":  len(raw),
			"line":   len(lines),
			"column": len(utf16.Encode([]rune(lines[len(lines)-1]))),
		},
		"source", map[string]any{"startOffset": 0, "endOffset": len(raw)},
	)
	if frontMatter.ExplicitLanguage != nil {
		node.Set("explicitLanguage", *frontMatter.ExplicitLanguage)
	}
	return node, replaceNonLineBreaksWithSpace(raw) + text[len(raw):]
}
