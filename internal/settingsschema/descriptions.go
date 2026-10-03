package settingsschema

import "github.com/system-inc/cohere/internal/format/formatoptions"

// The prose a person reads about each key. The keys themselves come from the loader; the test refuses a
// key here the loader does not accept, and a key the loader accepts that is not here.

var topLevelDescriptions = map[string]string{
	"$schema": "The schema an editor validates this file against. cohere does not read it.",

	"extends": "What this file builds on: a path to another settings file, relative to this one, or a rule " +
		"set cohere carries, named cohere:<name>, or a list of them applied in order. Their rules, overrides " +
		"and ignore patterns apply first and this file's after, so a shared tier holds most of the " +
		"configuration and a project states only what differs. A base two entries share is read once, " +
		"outermost.",

	"rules": "Each rule's severity, or [severity, ...options] for a rule that takes options. A rule this file " +
		"sets differently from the file it extends must be named under departures with the reason, and a rule " +
		"it turns off otherwise says why under reasons.",

	"departures": "For each rule this file sets differently from the file it extends, the reason why, in a " +
		"sentence. cohere reports every departure on every run, so it stays visible rather than becoming a " +
		"quiet exception. An entry that departs from nothing is refused.",

	"reasons": "For each rule this file turns off in its own rules, why, in a sentence. cohere prints the reason " +
		"beside the rule wherever coverage names it, so an off reads as a decision rather than an allowance, and " +
		"refuses to load an off no file explains. An off that departs from the file it extends says why under " +
		"departures instead. An entry for a rule this file does not turn off is refused.",

	"overrides": "Blocks that change rules for the paths their globs match, applied in order after the base " +
		"rules, a later block winning.",

	"cohere": "The cohere releases this project accepts, as an npm-style range (\"^1.0.0\", \"~1.4.2\", " +
		"\">=1.2.0 <2.0.0\"). A release outside it refuses to run and names both versions, since rule names, options " +
		"and sets are part of a release's contract. Only the project's own file may pin, never a file it extends.",

	"ignorePatterns": "Paths cohere never checks and the formatter never offers, one list for both, as globs " +
		"relative to the directory of the settings file cohere reads first: * within a path segment, ** across " +
		"any number of segments, ? one character, {a,b} alternation. A file's patterns follow those of the " +
		"file it extends.",

	"plugins": "Rule namespaces whose rules apply without a rules entry naming them, at warn, as an oxlint " +
		"plugins declaration does. A rules entry still sets any of them explicitly.",

	"jsPlugins": "Accepted and ignored: paths to the JavaScript rule implementations oxlint loads. cohere's " +
		"rules are compiled in, so there is nothing to load.",

	"settings": "Per-plugin settings carried for the JavaScript tools that still read this file. Allowed only " +
		"in the file cohere reads first, never in a file another extends, and nothing in cohere reads it.",

	FormatKey: "The formatter's options and the house ignore list. Only the Nexus tier holds this block, " +
		formatoptions.NexusTierFileName + " or the rule set cohere carries as " + formatoptions.NexusTierSetName +
		": a format key in any other settings file is refused, naming the file, so every repository formats " +
		"the same way. An option outside this list is refused rather than ignored.",
}

var overrideDescriptions = map[string]string{
	"files": "The paths the block applies to, as globs relative to the directory of the settings file cohere " +
		"reads first. A pattern that matches no file is refused, so a misspelled glob cannot pass as a no-op.",
	"rules": "Rule settings for the matched paths, in the same shape as the top-level rules.",
	"reason": "Why the block exists. cohere prints every reason on every run, so a block standing in for " +
		"unfinished work stays visible until that work lands.",
}

var formatDescriptions = map[string]string{
	"printWidth":      "The line width the printer wraps at.",
	"tabWidth":        "Spaces per indentation level.",
	"useTabs":         "Indent with tabs rather than spaces.",
	"semi":            "End statements with semicolons.",
	"singleQuote":     "Prefer single quotes for strings.",
	"trailingComma":   "Where multi-line lists keep a trailing comma.",
	"bracketSpacing":  "Spaces inside object braces: { a } rather than {a}.",
	"bracketSameLine": "Put the > of a multi-line JSX element at the end of its last line.",
	"arrowParens":     "Parenthesize a sole arrow-function parameter always, or avoid it where possible.",
	"endOfLine":       "Line endings. Only lf is supported.",
	"ignore": "Paths no repository formats, read as lines of an ignore file: a bare name matches at any " +
		"depth, a trailing slash a directory, and a glob without a slash the file's base name. The format " +
		"walk reads it after .gitignore and before the project's ignorePatterns.",
}
