// Package file_name_words is what a file's name says in words, and the two judgments of those words every engine
// shares, in one place (#2xt6v5r): a file name holds no `util` or `utils`, as a folder's name holds neither, and its
// words abbreviate nothing policy/Abbreviations.json names, as an identifier's do not.
//
// The TypeScript rules nexus/consistency-no-utils-file-name and nexus/consistency-no-abbreviated-file-name and the Go
// engine's rules of the same names read it, so which parts of a name are words, and what each judgment makes of them,
// are decided once. Swift's two rules port it, held to the same verdicts by the text rules corpus (families
// `utils-file-name` and `abbreviated-file-name`), and read the same vocabulary compiled in. Each engine decides only
// its own language's facts first: which files are its own to judge, and where a finding sits. It lives in its own
// package, importing nothing heavier than policy and the vocabulary, because the nexus rules package links the
// TypeScript checker and the Go engine must not.
//
// A file name is read in three steps:
//
//   - What a tool reads off the name is set aside, since it is the tool's grammar rather than a word the author chose:
//     the extension; Go's `_test` and its build-constraint suffixes (`_linux`, `_amd64`, `_linux_amd64`), as go/build
//     reads them; and a dotted role marker (`.test`, `.spec`, `.stories`, `.d`, `.config`) in TypeScript.
//   - A name nobody inside the project chose is never judged: a generated file's (the `.generated` marker), and one a
//     tool requires by its exact spelling (Go's `doc.go`, SwiftPM's `Package.swift`). TypeScript's framework names
//     are the TypeScript rule's to decide, from internal/lint/ecmascript/nextjs, which the Go engine must not link.
//   - What is left is cut into words: each run of ASCII letters, cut again where folder_words.IsCaseBoundary says a
//     word ends, so `string_utils`, `stringUtils`, `StringUtils` and `string-utils` are all `string` and `utils`.
package file_name_words

import (
	"strings"

	"github.com/system-inc/cohere/internal/folder_words"
	"github.com/system-inc/cohere/internal/naming/abbreviation"
)

// Language is the language a file is written in, whose conventions say which parts of its name are words.
type Language string

const (
	// LanguageTypeScript is a TypeScript or JavaScript file, judged by the TypeScript engine.
	LanguageTypeScript Language = "TypeScript"
	// LanguageGo is a Go file, judged by the Go engine.
	LanguageGo Language = "Go"
	// LanguageSwift is a Swift file, judged by the Swift engine.
	LanguageSwift Language = "Swift"
)

// generatedMarker is the dotted part the house puts between a generated file's name and its extension, in TypeScript
// and Swift alike: `PolicyWords.generated.swift`, `GraphQlSchema.generated.ts`. A generator chose the name, and
// renaming it is a change to the generator, so the file is not judged. Go marks a generated file in its text instead,
// which the Go rules read with go/ast.IsGenerated.
const generatedMarker = "generated"

// typeScriptRoleMarkers are the dotted parts of a TypeScript file's name that say what role the file plays for a tool,
// never what it holds: a test runner finds `.test` and `.spec`, Storybook `.stories` and `.story`, the compiler `.d`,
// and a tool configured by a file it finds by name, `next.config.ts` or `vitest.config.ts`, `.config`. Any other dotted
// part is the author's, and is judged, so a dot never hides a word.
var typeScriptRoleMarkers = map[string]bool{
	"test":    true,
	"spec":    true,
	"stories": true,
	"story":   true,
	"d":       true,
	"config":  true,
}

// goTestSuffix ends the stem of every file go test builds and go build leaves out.
const goTestSuffix = "_test"

// goToolNames are the Go file names a tool reads by their exact spelling: `doc.go`, where godoc and the Go project's
// own convention put a package's documentation.
var goToolNames = map[string]bool{
	"doc.go": true,
}

// swiftToolStems are the Swift stems a tool reads by their exact spelling: SwiftPM's manifest `Package`, and `main`,
// which SwiftPM gives meaning to as a target's top-level code. A version-specific manifest, `Package@swift-6.0.swift`,
// is the manifest too (swiftPackageManifestPrefix).
var swiftToolStems = map[string]bool{
	"Package": true,
	"main":    true,
}

// swiftPackageManifestPrefix opens the name of a version-specific SwiftPM manifest.
const swiftPackageManifestPrefix = "Package@swift"

// KnownOperatingSystems and KnownArchitectures are go/build's own lists, copied from internal/syslist at Go 1.27,
// which the toolchain reads a file name's suffix against. They are the toolchain's grammar rather than a house
// choice, and like the toolchain's they only grow: an entry stays after its port is gone, so a file named for it
// keeps meaning the same thing. cohere-go/consistency-require-matching-file-name reads them too.
var KnownOperatingSystems = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
	"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true,
	"plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
}

// KnownArchitectures is go/build's list of architectures, beside KnownOperatingSystems.
var KnownArchitectures = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true, "arm64be": true,
	"loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "mips64p32": true,
	"mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true, "s390": true,
	"s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

// ToolchainSuffix is the build constraint suffix a Go file's stem ends with, read as go/build reads it, or empty: an
// operating system, an architecture, or an operating system and then an architecture, after an underscore. go/build
// sets aside everything before the first underscore, so linux.go alone is no constraint. The stem is the file's name
// before its first dot, with any `_test` already removed.
func ToolchainSuffix(stem string) string {
	_, rest, found := strings.Cut(stem, "_")
	if !found {
		return ""
	}
	parts := strings.Split(rest, "_")
	count := len(parts)
	if count >= 2 && KnownOperatingSystems[parts[count-2]] && KnownArchitectures[parts[count-1]] {
		return "_" + parts[count-2] + "_" + parts[count-1]
	}
	if KnownOperatingSystems[parts[count-1]] || KnownArchitectures[parts[count-1]] {
		return "_" + parts[count-1]
	}
	return ""
}

// foreignFolders are folders whose files are someone else's, or input to a test rather than the project's source,
// wherever they sit: Node's installed packages, Go's vendored modules, the house's vendored third-party code, and Go's
// test inputs, which the go tool never builds and which hold names written to break the rules they test.
var foreignFolders = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"third_party":  true,
	"testdata":     true,
}

// IsForeignFolder reports whether a folder inside the project holds files whose names are not the project's to judge.
func IsForeignFolder(folder string) bool {
	return foreignFolders[folder]
}

// InForeignFolder reports whether any of a file's folders inside the project is a foreign one (IsForeignFolder).
func InForeignFolder(folders []string) bool {
	for _, folder := range folders {
		if IsForeignFolder(folder) {
			return true
		}
	}
	return false
}

// GoModuleFolders are the folders of a Go package's import path below its module's own path, outermost first, read
// from the import path rather than from a file's, which for a cgo package is a translation in the build cache. The
// module's own path is nobody's inside it to judge. With no module path, as in analysistest's `GOPATH` layout, the
// import path is already relative to its root, so all of it is the package's folders.
func GoModuleFolders(importPath string, modulePath string) []string {
	if modulePath == "" {
		return strings.Split(importPath, "/")
	}
	if importPath == modulePath {
		return nil
	}
	return strings.Split(strings.TrimPrefix(importPath, modulePath+"/"), "/")
}

// Words are the words of a file's name in its language, and false when the name is not judged at all: a generated
// file's, or one a tool requires. The name is the file's own, without its folder.
func Words(fileName string, language Language) ([]string, bool) {
	var parts []string
	switch language {
	case LanguageGo:
		if goToolNames[fileName] {
			return nil, false
		}
		stem, _, _ := strings.Cut(fileName, ".")
		stem = strings.TrimSuffix(stem, goTestSuffix)
		parts = []string{strings.TrimSuffix(stem, ToolchainSuffix(stem))}
	case LanguageTypeScript:
		dotted := strings.Split(fileName, ".")
		// The last part is the extension, unless the name has no dot at all.
		if len(dotted) > 1 {
			dotted = dotted[:len(dotted)-1]
		}
		for index, part := range dotted {
			if part == generatedMarker {
				return nil, false
			}
			// A role marker follows the name, so the first part is always the name: config.ts is named config.
			if index == 0 || !typeScriptRoleMarkers[part] {
				parts = append(parts, part)
			}
		}
	case LanguageSwift:
		stem := strings.TrimSuffix(fileName, ".swift")
		if swiftToolStems[stem] || strings.HasPrefix(stem, swiftPackageManifestPrefix) {
			return nil, false
		}
		// Swift's dots name a nested type, `Outer.Inner+Purpose.swift`, so every dotted part is a word but the marker.
		for part := range strings.SplitSeq(stem, ".") {
			if part == generatedMarker {
				return nil, false
			}
			parts = append(parts, part)
		}
	}
	words := []string{}
	for _, part := range parts {
		words = append(words, wordsOf(part)...)
	}
	return words, true
}

// wordsOf is text cut into words: each run of ASCII letters, cut again before a capital where
// folder_words.IsCaseBoundary says a word ends. Anything else, an underscore, a hyphen, a plus, a digit, separates two
// words and is none, so a name's leading underscores and its digits are never part of a word.
func wordsOf(text string) []string {
	words := []string{}
	start := -1
	for index := 0; index <= len(text); index++ {
		if index == len(text) || !isLetter(text[index]) {
			if start >= 0 {
				words = append(words, text[start:index])
			}
			start = -1
			continue
		}
		if start < 0 {
			start = index
			continue
		}
		if folder_words.IsCaseBoundary(text, index) {
			words = append(words, text[start:index])
			start = index
		}
	}
	return words
}

func isLetter(character byte) bool {
	return ('a' <= character && character <= 'z') || ('A' <= character && character <= 'Z')
}

// IsUtils reports whether a name, lowercased and with its leading underscores removed, is `util` or `utils`. It is
// the one reading of the word every Go-hosted rule shares, the folder rules' and the file rules', and Swift's
// ConsistencyNoUtilsFolder.isBanned ports it.
//
// The name is matched whole: `utilities`, `myutils` and `utilsomething` pass, since a rule that fires on the spelling
// it is asking for is worse than no rule.
func IsUtils(name string) bool {
	switch strings.ToLower(strings.TrimLeft(name, "_")) {
	case "util", "utils":
		return true
	}
	return false
}

// UtilsWord is the first of a file name's words that is `util` or `utils`, as the name spells it, and false when none
// is. A word right after `no` is the name of a ban on it, not a name for nothing in particular: a house rule is named
// `<category>-no-<object>`, so consistency_no_utils_folder.go and ConsistencyNoUtilsFolderTests.swift are named for
// exactly what they hold, the rule refusing the word, and mention it as a quotation would.
func UtilsWord(words []string) (string, bool) {
	for index, word := range words {
		if IsUtils(word) && (index == 0 || !strings.EqualFold(words[index-1], "no")) {
			return word, true
		}
	}
	return "", false
}

// UtilitiesReplacement is the house spelling for a `util` or `utils` word, in the casing its letters have: `utilities`,
// `Utilities` where it opens with a capital, and `UTILITIES` where every letter is one.
func UtilitiesReplacement(word string) string {
	switch {
	case word == strings.ToUpper(word):
		return "UTILITIES"
	case strings.HasPrefix(word, "U"):
		return "Utilities"
	}
	return "utilities"
}

// Identifier is a file name's words written as one camelCase identifier, `msgPrinter` for `msg_printer.go`,
// `MsgPrinter.swift` and `msg-printer.ts` alike, the shape the abbreviation vocabulary reads a name in: the first word
// lowercased, each after it capitalized and the rest of it lowercased. Every convention's name is read through the one
// spelling, so a word is judged wherever it sits, as the identifier holding the same words would judge it.
//
// A word of two or more capitals and no lowercase letter is kept as written: it is an initialism or a code, as `CH`
// in `FlagCH.tsx` is Switzerland's, and the vocabulary reads an initialism in capitals as no abbreviation of its, as
// the identifier rule passes `FlagCH`. Lowercased, it would read as the word `ch`.
func Identifier(words []string) string {
	var identifier strings.Builder
	for index, word := range words {
		switch {
		case isInitialism(word):
			identifier.WriteString(word)
		case index == 0:
			identifier.WriteString(strings.ToLower(word))
		default:
			identifier.WriteString(abbreviation.Capitalize(strings.ToLower(word)))
		}
	}
	return identifier.String()
}

// isInitialism reports whether a word is two or more capitals and nothing else.
func isInitialism(word string) bool {
	return len(word) >= 2 && strings.ToUpper(word) == word && strings.ToLower(word) != word
}

// Abbreviated is what policy/Abbreviations.json says of a file name's words, read as one identifier (Identifier), and
// false when it says nothing. A run of words that spells a registered rule's name is the rule's, so only the words
// around it are judged (withoutRuleName). The finding's entry names the word and how to spell it out; its message and
// values are the identifier rule's, and a file-name rule renders its own from the entry (Advice).
func Abbreviated(words []string, ruleNames RuleNames) (abbreviation.Finding, bool) {
	ownWords := ruleNames.withoutRuleName(words)
	if len(ownWords) == 0 {
		return abbreviation.Finding{}, false
	}
	return abbreviation.Policy.Find(Identifier(ownWords))
}

// The ids of the two messages a file-name abbreviation finding carries, in
// policy/messages/consistency-no-abbreviated-file-name.json.
const (
	// AbbreviatedFileName is a word with expansions, with the values `fileName`, `word` and `expansions`.
	AbbreviatedFileName = "abbreviatedFileName"
	// AbbreviatedFileNameAdvice is a word whose entry gives advice in place of expansions, with the values
	// `fileName`, `word` and `advice`.
	AbbreviatedFileNameAdvice = "abbreviatedFileNameAdvice"
)

// Advice is the message id and values for a file name the vocabulary found abbreviated: the word, lowercase as the
// vocabulary spells it, and its expansions quoted as a list, every meaning named and the reader asked for whichever
// it means where there is more than one, or the entry's advice where it has no expansion to give.
func Advice(fileName string, entry *abbreviation.Entry) (string, map[string]string) {
	if len(entry.Expansions) == 0 {
		return AbbreviatedFileNameAdvice, map[string]string{
			"fileName": fileName,
			"word":     entry.Abbreviation,
			"advice":   entry.Advice,
		}
	}
	quoted := make([]string, len(entry.Expansions))
	for index, expansion := range entry.Expansions {
		quoted[index] = `"` + expansion + `"`
	}
	expansions := quoted[0]
	if len(quoted) > 1 {
		expansions = strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1] +
			", whichever it means"
	}
	return AbbreviatedFileName, map[string]string{
		"fileName":   fileName,
		"word":       entry.Abbreviation,
		"expansions": expansions,
	}
}
