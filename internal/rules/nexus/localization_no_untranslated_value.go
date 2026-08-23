package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/ecmascript/imports"
)

// minimumLengthToFlagAsUntranslated is how long a string must be before matching English is taken
// as evidence it was never translated.
//
// Short strings are genuinely the same in many languages. "OK", "Email", and a bare "%" are correct
// translations of themselves, and flagging them teaches translators that the rule cries wolf, which
// costs more than the handful of real findings it would add.
const minimumLengthToFlagAsUntranslated = 4

// untranslatedValuePreviewLength is how much of a value a finding quotes before eliding.
const untranslatedValuePreviewLength = 40

func messageMissingTranslation(key string, locale string) rule.Message {
	return rule.Message{
		Id: "missingTranslation",
		Description: "Translation for '" + key + "' is empty while English has content. An empty " +
			"string is not a missing translation at runtime, it is a blank rendered where a word " +
			"belongs, so provide a '" + locale + "' translation or remove the key.",
	}
}

func messageIdenticalToSource(key string, locale string, value string) rule.Message {
	preview := value
	if len(preview) > untranslatedValuePreviewLength {
		preview = preview[:untranslatedValuePreviewLength] + "..."
	}
	return rule.Message{
		Id: "identicalToSource",
		Description: "Translation for '" + key + "' is identical to English (\"" + preview +
			"\"). A copied English string reads as a finished translation to everything downstream, " +
			"so it never appears in a coverage count and nobody is told the '" + locale +
			"' reader is still seeing English.",
	}
}

// LocalizationNoUntranslatedValue requires a locale file to hold real translations rather than empty
// strings or copied English.
//
//	valid:   export default { Greeting: 'Hola' }        beside an en.ts saying 'Hello'
//	invalid: export default { Greeting: '' }            English has content
//	invalid: export default { Greeting: 'Hello' }       identical to English
//
// Both defects are invisible in the ordinary places a team would look. An empty value renders a
// blank rather than raising, and a copied English string is indistinguishable from a finished
// translation to every tool that counts keys, so a locale can report full coverage while showing a
// reader English. That is the whole reason this is a lint rule and not a spreadsheet review.
//
// The comparison is against the sibling en.ts, read from the program rather than from disk. The
// TypeScript original reads the file with `fs.readFileSync`, parses it with a second parser
// instance, and memoizes the result in a module-level cache keyed by directory. Every part of that
// is machinery we do not need: en.ts is a .ts file in the same directory, so it is already in the
// program and already parsed. Asking the program for it costs a map lookup, it cannot go stale
// against the file being linted, and it does not hold parsed ASTs alive for the process lifetime.
//
// A file whose en.ts is not in the program is declined rather than reported on. That is the
// conservative direction on purpose: the alternative is reporting every key in a locale file as
// untranslatable because the comparison basis was missing, which is a wall of findings that says
// nothing about the translations.
//
// No fix. The repair is a translation, which a rule cannot write.
var LocalizationNoUntranslatedValue = rule.Rule{
	Name: "localization-no-untranslated-value",

	// Reads the English translation table out of the program, so a findings cache keyed on the
	// linted file alone would go stale when en.ts changes and this file does not.
	ReadsProgram: true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		localeCode, translationsDirectory, isTranslationFile := translationFileInformation(ctx.SourceFile)
		if !isTranslationFile {
			// Deciding on the path alone, before any node is visited, is what keeps this rule free
			// on the overwhelming majority of files that are not locale data.
			return nil
		}

		if ctx.Program == nil {
			return nil
		}
		englishSourceFile := ctx.Program.GetSourceFile(translationsDirectory + "/en.ts")
		if englishSourceFile == nil {
			return nil
		}
		englishValues := map[string]string{}
		if englishObject := exportedObjectLiteral(englishSourceFile); englishObject != nil {
			collectStringValues(englishObject, "", englishValues)
		}
		if len(englishValues) == 0 {
			return nil
		}

		return rule.Listeners{
			ast.KindExportAssignment: func(node *ast.Node) {
				assignment := node.AsExportAssignment()
				// `export = thing` is a different statement wearing the same node kind, and it is
				// not the shape a translation file uses.
				if assignment == nil || assignment.IsExportEquals || assignment.Expression == nil {
					return
				}

				objectLiteral := resolveObjectLiteral(assignment.Expression, ctx.SourceFile)
				if objectLiteral == nil {
					return
				}

				reportUntranslated(ctx, objectLiteral, "", englishValues, localeCode)
			},
		}
	},
}

// reportUntranslated walks a locale object against the English values, reporting as it goes.
//
// Nested objects recurse with a dotted prefix, so a key path in a finding reads the same way it is
// written in the file.
func reportUntranslated(
	ctx rule.Context,
	objectLiteral *ast.Node,
	prefix string,
	englishValues map[string]string,
	localeCode string,
) {
	forEachStringProperty(objectLiteral, prefix, func(key string, value string, valueNode *ast.Node) {
		englishValue, hasEnglishValue := englishValues[key]
		// A key English does not have is not this rule's business. It may be locale-specific, or it
		// may be stale, and either way the comparison this rule performs has no basis.
		if !hasEnglishValue {
			return
		}

		if strings.TrimSpace(value) == "" && strings.TrimSpace(englishValue) != "" {
			ctx.ReportNode(valueNode, messageMissingTranslation(key, localeCode))
			return
		}

		if value == englishValue && len(englishValue) >= minimumLengthToFlagAsUntranslated {
			ctx.ReportNode(valueNode, messageIdenticalToSource(key, localeCode, englishValue))
		}
	}, func(key string, nested *ast.Node) {
		reportUntranslated(ctx, nested, key, englishValues, localeCode)
	})
}

// collectStringValues flattens an object literal into dotted key paths, which is how the English
// side is read.
func collectStringValues(objectLiteral *ast.Node, prefix string, into map[string]string) {
	forEachStringProperty(objectLiteral, prefix, func(key string, value string, valueNode *ast.Node) {
		into[key] = value
	}, func(key string, nested *ast.Node) {
		collectStringValues(nested, key, into)
	})
}

// forEachStringProperty visits one object literal, handing string properties to onString and nested
// object properties to onObject, both with the dotted key path already built.
//
// One traversal serves both sides of the comparison. Writing the walk twice is how the English
// reader and the locale reader drift into disagreeing about what counts as a key, which would show
// up as findings on keys that do exist.
func forEachStringProperty(
	objectLiteral *ast.Node,
	prefix string,
	onString func(key string, value string, valueNode *ast.Node),
	onObject func(key string, nested *ast.Node),
) {
	literal := objectLiteral.AsObjectLiteralExpression()
	if literal == nil || literal.Properties == nil {
		return
	}

	for _, property := range literal.Properties.Nodes {
		// Shorthand properties, spreads, methods, and getters carry no literal string to compare.
		// The original filters to `Property` for the same reason.
		if property.Kind != ast.KindPropertyAssignment {
			continue
		}
		assignment := property.AsPropertyAssignment()
		if assignment == nil || assignment.Initializer == nil {
			continue
		}

		keyName, hasKeyName := propertyKeyName(assignment.Name())
		if !hasKeyName {
			continue
		}
		fullKey := keyName
		if prefix != "" {
			fullKey = prefix + "." + keyName
		}

		initializer := assignment.Initializer
		switch {
		case ast.IsStringLiteralLike(initializer):
			onString(fullKey, initializer.Text(), initializer)
		case initializer.Kind == ast.KindObjectLiteralExpression:
			onObject(fullKey, initializer)
		}
	}
}

// propertyKeyName reads the key of a property, whether it was written bare or quoted.
//
// A computed key is skipped: its value is not known from the syntax, so there is no key path to
// compare against English.
func propertyKeyName(keyNode *ast.Node) (string, bool) {
	if keyNode == nil {
		return "", false
	}
	switch {
	case ast.IsIdentifier(keyNode):
		return keyNode.Text(), true
	case ast.IsStringLiteralLike(keyNode), ast.IsNumericLiteral(keyNode):
		return keyNode.Text(), true
	}
	return "", false
}

// exportedObjectLiteral finds the object a file default-exports, or nil.
func exportedObjectLiteral(sourceFile *ast.SourceFile) *ast.Node {
	if sourceFile == nil {
		return nil
	}
	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindExportAssignment {
			continue
		}
		assignment := statement.AsExportAssignment()
		if assignment == nil || assignment.IsExportEquals || assignment.Expression == nil {
			continue
		}
		if objectLiteral := resolveObjectLiteral(assignment.Expression, sourceFile); objectLiteral != nil {
			return objectLiteral
		}
	}
	return nil
}

// resolveObjectLiteral unwraps an exported expression down to the object literal underneath.
//
// Four shapes reach here, and all four appear in real translation files:
//
//	export default { ... }
//	export default { ... } satisfies TranslationsType
//	export default { ... } as TranslationsType
//	const Spanish = { ... } as const; export default Spanish
//
// The last one exists because some locale files need their literal-string types preserved through an
// intermediate binding, so a slot validator can compare placeholders against the contract, before
// the widening assertion at export. Following the identifier is what keeps those files linted
// rather than silently skipped, and a file this rule silently skips is a file whose translations
// nobody is checking.
func resolveObjectLiteral(expression *ast.Node, sourceFile *ast.SourceFile) *ast.Node {
	current := unwrapTypeAssertions(expression)
	if current == nil {
		return nil
	}
	if current.Kind == ast.KindObjectLiteralExpression {
		return current
	}

	if ast.IsIdentifier(current) && sourceFile != nil {
		return topLevelObjectLiteralBinding(current.Text(), sourceFile)
	}
	return nil
}

// topLevelObjectLiteralBinding finds `const name = { ... }` among a file's top-level statements.
//
// Only the top level is searched, matching the original. A binding declared inside a block is not
// something an export at the top level could have referred to.
func topLevelObjectLiteralBinding(name string, sourceFile *ast.SourceFile) *ast.Node {
	for _, statement := range sourceFile.Statements.Nodes {
		if statement.Kind != ast.KindVariableStatement {
			continue
		}
		variableStatement := statement.AsVariableStatement()
		if variableStatement == nil || variableStatement.DeclarationList == nil {
			continue
		}
		declarationList := variableStatement.DeclarationList.AsVariableDeclarationList()
		if declarationList == nil || declarationList.Declarations == nil {
			continue
		}
		for _, declaration := range declarationList.Declarations.Nodes {
			variableDeclaration := declaration.AsVariableDeclaration()
			if variableDeclaration == nil || variableDeclaration.Initializer == nil {
				continue
			}
			declarationName := variableDeclaration.Name()
			if declarationName == nil || !ast.IsIdentifier(declarationName) || declarationName.Text() != name {
				continue
			}
			initializer := unwrapTypeAssertions(variableDeclaration.Initializer)
			if initializer != nil && initializer.Kind == ast.KindObjectLiteralExpression {
				return initializer
			}
		}
	}
	return nil
}

// unwrapTypeAssertions strips `as T` and `satisfies T` wrappers, which are type-only and leave the
// value underneath unchanged.
//
// Repeatedly, because `{ ... } as const satisfies TranslationsType` wears both.
func unwrapTypeAssertions(expression *ast.Node) *ast.Node {
	current := expression
	for current != nil {
		switch current.Kind {
		case ast.KindAsExpression:
			asExpression := current.AsAsExpression()
			if asExpression == nil {
				return current
			}
			current = asExpression.Expression
		case ast.KindSatisfiesExpression:
			satisfiesExpression := current.AsSatisfiesExpression()
			if satisfiesExpression == nil {
				return current
			}
			current = satisfiesExpression.Expression
		case ast.KindParenthesizedExpression:
			parenthesized := current.AsParenthesizedExpression()
			if parenthesized == nil {
				return current
			}
			current = parenthesized.Expression
		default:
			return current
		}
	}
	return nil
}

// translationFileInformation decides whether a file is locale data this rule should read, returning
// its locale code and the directory its en.ts sits in.
//
// Every exclusion here is load-bearing, and each one is a file that would otherwise be compared
// against itself or against nothing:
//
//   - en.ts is the basis of the comparison, so every value in it is identical to English by
//     definition and the rule would report the entire file.
//   - index.ts and locales.ts are wiring rather than translations.
//   - *Translations.ts, *TranslationsType.ts, and *Interface.ts are type declarations that happen to
//     live in the same directory.
//
// The directory match is on a whole path segment. Matching a bare substring would pull in a
// directory named "translations-archive" and, worse, would silently start linting whatever a future
// directory named that way holds.
func translationFileInformation(sourceFile *ast.SourceFile) (string, string, bool) {
	fileName := imports.NormalizedFileName(sourceFile)
	if !strings.HasSuffix(fileName, ".ts") {
		return "", "", false
	}

	lastSlash := strings.LastIndex(fileName, "/")
	if lastSlash < 0 {
		return "", "", false
	}
	directory := fileName[:lastSlash]
	baseName := fileName[lastSlash+1:]

	if baseName == "en.ts" || baseName == "index.ts" || baseName == "locales.ts" ||
		strings.HasSuffix(baseName, "Translations.ts") ||
		strings.HasSuffix(baseName, "TranslationsType.ts") ||
		strings.HasSuffix(baseName, "Interface.ts") {
		return "", "", false
	}

	isInTranslationsDirectory := false
	for _, segment := range strings.Split(directory, "/") {
		if segment == "_translations" || segment == "translations" {
			isInTranslationsDirectory = true
			break
		}
	}
	if !isInTranslationsDirectory {
		return "", "", false
	}

	return strings.TrimSuffix(baseName, ".ts"), directory, true
}
