package main

import (
	"path"
	"strings"
	"unicode"
)

// operatingSystems and architectures are the file name suffixes the go command reads as build
// constraints (go/build's knownOS and knownArch).
var operatingSystems = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
	"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true,
	"openbsd": true, "plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
}

var architectures = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true,
	"arm64be": true, "loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true,
	"mips64p32": true, "mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true,
	"riscv64": true, "s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}

// fileNameParts splits "poller_linux_amd64_test.go" into its base "poller" and the suffix the go
// command reads as constraints, "_linux_amd64_test", following go/build's goodOSArchFile.
func fileNameParts(name string) (base string, suffix string) {
	stem := strings.TrimSuffix(name, ".go")
	isTest := strings.HasSuffix(stem, "_test")
	stem = strings.TrimSuffix(stem, "_test")
	parts := strings.Split(stem, "_")
	count := len(parts)
	constraint := ""
	if count >= 3 && operatingSystems[parts[count-2]] && architectures[parts[count-1]] {
		constraint = "_" + parts[count-2] + "_" + parts[count-1]
		parts = parts[:count-2]
	} else if count >= 2 && (operatingSystems[parts[count-1]] || architectures[parts[count-1]]) {
		constraint = "_" + parts[count-1]
		parts = parts[:count-1]
	}
	base = strings.Join(parts, "_")
	suffix = constraint
	if isTest {
		suffix += "_test"
	}
	return base, suffix
}

// snakeCase turns swiftRun into swift_run, HTTPServer into http_server and ParseJSON into parse_json.
// A plural acronym stays whole: UserIDs is user_ids.
func snakeCase(name string) string {
	runes := []rune(name)
	var builder strings.Builder
	for index, character := range runes {
		if !unicode.IsUpper(character) {
			builder.WriteRune(character)
			continue
		}
		if index > 0 {
			previous := runes[index-1]
			nextIsLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
			pluralAcronym := index+1 < len(runes) && runes[index+1] == 's' &&
				(index+2 == len(runes) || unicode.IsUpper(runes[index+2]))
			if unicode.IsLower(previous) || unicode.IsDigit(previous) ||
				(unicode.IsUpper(previous) && nextIsLower && !pluralAcronym) {
				builder.WriteByte('_')
			}
		}
		builder.WriteRune(unicode.ToLower(character))
	}
	return builder.String()
}

// assumedImportName is the name an import without an explicit name is assumed to bind, as goimports
// assumes it: the last path element, skipping a major version element, without a "go-" prefix, cut at
// the first character that cannot be in an identifier.
func assumedImportName(importPath string) string {
	base := path.Base(importPath)
	if isMajorVersion(base) && path.Dir(importPath) != "." {
		base = path.Base(path.Dir(importPath))
	}
	base = strings.TrimPrefix(base, "go-")
	if index := strings.IndexFunc(base, func(character rune) bool {
		return !(unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_')
	}); index >= 0 {
		base = base[:index]
	}
	return base
}

func isMajorVersion(element string) bool {
	if len(element) < 2 || element[0] != 'v' {
		return false
	}
	for _, character := range element[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
