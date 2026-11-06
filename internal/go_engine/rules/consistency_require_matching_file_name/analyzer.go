// Package consistency_require_matching_file_name is the Go engine's rule that a file is named for the type it serves.
//
// It is the Go form of the house rule its TypeScript and Swift twins enforce: Button.tsx holds Button, and
// Registry.swift holds Registry. Go idiom names files freely, several types to a file, but the house serves one type
// with methods per file (consistency-require-one-type-per-file), and once a file does, its name is that type's in
// snake_case. Then the tree is an index both ways: a reader looking for Registry opens registry.go, and a reader
// seeing registry.go knows what it holds without opening it.
package consistency_require_matching_file_name

import (
	"go/ast"
	"go/build/constraint"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/served"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-require-matching-file-name"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_require_matching_file_name",
	Doc: "Flags a file that serves exactly one type with methods and is not named for it: a file declaring the type " +
		"is its name in snake_case, with Go's own _GOOS, _GOARCH or _GOOS_GOARCH suffix if it has one, or one " +
		"trailing word naming its variant if it carries a //go:build line, registry_unix.go, and a file of only " +
		"its methods may add a theme, graph_walk.go for Graph. A test file, main.go in package main, and a file " +
		"serving no type or several are not judged, and a sum type's variant whose every method is an empty " +
		"marker serves nothing, as in the one type per file rule.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the types a file serves from package served, which
// resolves every receiver through the types and which the one type per file rule reads too, so the type a file is
// judged against is the one that rule counts.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var requireMatchingFileNameMessage = policy.MessageOf(Name, "requireMatchingFileName")

// noAlternative is the message for a file declaring its type, whose one right name is the type's own.
var noAlternative = requireMatchingFileNameMessage.Option("alternative", "none")

// methodFileAlternative is the message for a file of only a type's methods, which may also take a theme.
var methodFileAlternative = requireMatchingFileNameMessage.Option("alternative", "methodFile")

// knownOperatingSystems and knownArchitectures are go/build's own lists, copied from internal/syslist at Go 1.27,
// which the toolchain reads a file name's suffix against. They are the toolchain's grammar rather than a house
// choice, and like the toolchain's they only grow: an entry stays after its port is gone, so a file named for it
// keeps meaning the same thing.
var knownOperatingSystems = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
	"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true,
	"plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
}

var knownArchitectures = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true, "arm64be": true,
	"loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "mips64p32": true,
	"mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true, "s390": true,
	"s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

func run(pass *analysis.Pass) (any, error) {
	// The types a file serves come from package served, which consistency-require-one-type-per-file reads too, so the
	// type a file is judged against is the one that rule counts, a sum type's marker-only variants left out alike.
	index := served.NewIndex(pass)
	for _, file := range pass.Files {
		judgeFile(pass, file, index)
	}
	return nil, nil
}

// judgeFile reports the one type file serves when the file's name does not name it.
func judgeFile(pass *analysis.Pass, file *ast.File, index *served.Index) {
	// The name the file has on disk, not one a //line directive gives it, since the disk's name is the one a reader
	// searches for.
	fileName := filepath.Base(pass.Fset.PositionFor(file.Package, false).Filename)
	// A test file is named for the code it tests, and a helper type with methods inside it is scaffolding for that
	// subject, so claiming the file's name for the helper would be wrong. The TypeScript twin leaves its test and
	// stories files alone for the same reason.
	if strings.HasSuffix(fileName, "_test.go") {
		return
	}
	// The program's entry point is main.go's main declaration, whatever helper type sits beside it, as the Swift
	// twin leaves main.swift alone.
	if fileName == "main.go" && pass.Pkg.Name() == "main" {
		return
	}
	servedTypes := index.Types(file)
	// A file serving several types is the one type per file rule's finding, and which of them the file should be
	// named for is a guess until the split answers it.
	if len(servedTypes) != 1 {
		return
	}
	only := servedTypes[0]
	stem := fileStem(fileName)
	buildConstrained := hasBuildConstraint(file)
	allowedTail := isToolchainSuffix
	// A type split across build variants, registry_unix.go and registry_other.go, needs a name per variant, and a
	// build tag such as unix, other or cgo is no suffix go/build reads, so the file's //go:build line is what makes
	// its last word the variant's name rather than a theme.
	if buildConstrained {
		allowedTail = isVariantSuffix
	}
	if !only.Declared {
		allowedTail = isThemeSuffix
	}
	if namesType(stem, only.TypeName.Name(), allowedTail) {
		return
	}
	suffix := variantSuffix(stem, buildConstrained)
	values := map[string]string{
		"typeName":     only.TypeName.Name(),
		"fileName":     fileName,
		"typeFileName": snakeCase(only.TypeName.Name()) + suffix + ".go",
	}
	alternative := noAlternative
	if !only.Declared {
		values["themedFileName"] = snakeCase(only.TypeName.Name()) + "_<purpose>" + suffix + ".go"
		alternative = methodFileAlternative
	}
	pass.Report(analysis.Diagnostic{
		Pos:      only.Node.Pos(),
		End:      only.Node.End(),
		Category: requireMatchingFileNameMessage.Id,
		Message:  requireMatchingFileNameMessage.Render(values, alternative),
	})
}

// fileStem is a file's name up to its first dot, the part go/build reads a toolchain suffix from.
func fileStem(fileName string) string {
	stem, _, _ := strings.Cut(fileName, ".")
	return stem
}

// namesType is whether stem spells typeName, alone or followed by an underscore and a tail allowedTail accepts. The
// spelling is compared without case or underscores, so http_server and httpserver both name HttpServer, and
// url_parser names URLParser: where a word ends inside a run of capitals or beside a digit is a guess, and a file
// whose name plainly spells its type is never flagged over one.
func namesType(stem string, typeName string, allowedTail func(string) bool) bool {
	typeKey := spellingKey(typeName)
	if spellingKey(stem) == typeKey {
		return true
	}
	for index, character := range stem {
		if character != '_' {
			continue
		}
		head, tail := stem[:index], stem[index+1:]
		if tail != "" && spellingKey(head) == typeKey && allowedTail(tail) {
			return true
		}
	}
	return false
}

// spellingKey is text in lowercase with its underscores dropped.
func spellingKey(text string) string {
	return strings.Map(func(character rune) rune {
		if character == '_' {
			return -1
		}
		return unicode.ToLower(character)
	}, text)
}

// isToolchainSuffix is whether tail, the part of a file's name after the type's, is one go/build reads as a build
// constraint: an operating system, an architecture, or an operating system and then an architecture.
func isToolchainSuffix(tail string) bool {
	parts := strings.Split(tail, "_")
	switch len(parts) {
	case 1:
		return knownOperatingSystems[parts[0]] || knownArchitectures[parts[0]]
	case 2:
		return knownOperatingSystems[parts[0]] && knownArchitectures[parts[1]]
	default:
		return false
	}
}

// isVariantSuffix is whether tail names a build-constrained file's variant: a suffix go/build reads, or one word, the
// build tag the file is split by, as unix in registry_unix.go. Two words are a theme and a variant, which a file
// declaring its type does not take.
func isVariantSuffix(tail string) bool {
	return isToolchainSuffix(tail) || !strings.Contains(tail, "_")
}

// hasBuildConstraint is whether file carries a //go:build line before its package clause, where go/build reads one.
// The comments are read from the AST, so //go:build in a sentence, in a string, or after the package clause, where
// it constrains nothing, does not count.
func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		// The comment groups are in source order, so the first past the package clause ends the search.
		if group.Pos() >= file.Package {
			return false
		}
		for _, comment := range group.List {
			if constraint.IsGoBuild(comment.Text) {
				return true
			}
		}
	}
	return false
}

// isThemeSuffix accepts any tail, since a file of one type's methods may be named for their theme as well as their
// type, graph_walk.go beside graph.go, and a toolchain suffix is one more such tail.
func isThemeSuffix(string) bool {
	return true
}

// toolchainSuffix is the build constraint suffix stem ends with, read as go/build reads it, or empty. go/build sets
// aside everything before the first underscore, so linux.go alone is no constraint.
func toolchainSuffix(stem string) string {
	_, rest, found := strings.Cut(stem, "_")
	if !found {
		return ""
	}
	parts := strings.Split(rest, "_")
	count := len(parts)
	if count >= 2 && knownOperatingSystems[parts[count-2]] && knownArchitectures[parts[count-1]] {
		return "_" + parts[count-2] + "_" + parts[count-1]
	}
	if knownOperatingSystems[parts[count-1]] || knownArchitectures[parts[count-1]] {
		return "_" + parts[count-1]
	}
	return ""
}

// variantSuffix is the suffix a suggested name keeps, so following the suggestion never changes what the file builds
// for: the toolchain suffix stem ends with, or else, in a build-constrained file, its last word, which names the
// variant when anything does. The name it makes always passes the rule.
func variantSuffix(stem string, buildConstrained bool) string {
	if suffix := toolchainSuffix(stem); suffix != "" || !buildConstrained {
		return suffix
	}
	index := strings.LastIndex(stem, "_")
	// A stem with no underscore, or only a leading or trailing one, has no word after a name to keep.
	if index <= 0 || index == len(stem)-1 {
		return ""
	}
	return stem[index:]
}

// snakeCase is name in lowercase with an underscore at each word boundary: before a capital that follows a lowercase
// letter or a digit, and before the last capital of a run when a lowercase letter follows it, so HttpServer is
// http_server and URLParser is url_parser. It is the name a finding suggests, which namesType always accepts.
func snakeCase(name string) string {
	runes := []rune(name)
	var builder strings.Builder
	for index, character := range runes {
		if index > 0 && unicode.IsUpper(character) {
			previous := runes[index-1]
			lowerFollows := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && lowerFollows) {
				builder.WriteRune('_')
			}
		}
		builder.WriteRune(unicode.ToLower(character))
	}
	return builder.String()
}
