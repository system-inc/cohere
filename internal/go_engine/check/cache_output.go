package check

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// outputDigest is what an importer can read of a package, hashed: its export data written without positions, then
// every fact it exported, slot by slot in order of slot name, the object facts sorted by objectpath, type and bytes,
// the package facts by type and bytes. Two runs that give a package the same digest give every importer the same
// types and the same facts, so an importer whose own inputs did not move need not be analyzed again (see
// CacheSession.secondKey).
//
// Positions are left out because a body edit that moves a line moves every later declaration's position, which is in
// the export data, while what an importer's rules conclude does not depend on it. That rests on a census of every
// registered analyzer and what each requires, at x/tools v0.50.0 and staticcheck v0.8.1 and over the house rules: of
// everything that reads a position (Position, PositionFor, DisplayPosition, a token.Pos formatted into a message), only
// vet's structtag puts another package's position into a finding, a duplicated tag's "also at" file and line, which
// can be a field of a struct embedded from another package. The rest read positions of the package's own nodes
// (scannererr, sqlrowserr, hostport, lostcancel, SA5005), only to ask which of its own files a node is in
// (code.IsGenerated, code.IsInTest, the house rules' file maps), or as Related information (SA9008, SA4023 and
// others), which newFinding drops. And no fact type holds a position. The census sampled two kinds rather than
// reading every one: a diagnostic placed at an imported object, and a fix that edits another package's file. So the
// guard is positionDependent, which needs no list: a package with any such finding never takes the second key.
func outputDigest(exportData []byte, analyses map[string]storedAnalysis) cache.Key {
	parts := [][]byte{[]byte("output"), exportData}
	for _, slot := range slices.Sorted(maps.Keys(analyses)) {
		stored := analyses[slot]
		objectFacts := slices.Clone(stored.ObjectFacts)
		slices.SortFunc(objectFacts, func(left, right storedObjectFact) int {
			return cmp.Or(cmp.Compare(left.Path, right.Path), cmp.Compare(left.Fact.Type, right.Fact.Type),
				slices.Compare(left.Fact.Encoded, right.Fact.Encoded))
		})
		packageFacts := slices.Clone(stored.PackageFacts)
		slices.SortFunc(packageFacts, func(left, right storedFact) int {
			return cmp.Or(cmp.Compare(left.Type, right.Type), slices.Compare(left.Encoded, right.Encoded))
		})
		parts = append(parts, []byte(slot), []byte(strconv.Itoa(len(objectFacts))))
		for _, objectFact := range objectFacts {
			parts = append(parts, []byte(objectFact.Path), []byte(objectFact.Fact.Type), objectFact.Fact.Encoded)
		}
		parts = append(parts, []byte(strconv.Itoa(len(packageFacts))))
		for _, packageFact := range packageFacts {
			parts = append(parts, []byte(packageFact.Type), packageFact.Encoded)
		}
	}
	return cache.DigestOf(parts...)
}

// positionDependent reports whether a finding of saved can change with a line of another package: its message names
// a file the load holds that is not one of saved's own, by its absolute path or by its path relative to the finding's
// own directory, as structtag writes it, a trailing `:line` or `:line:column` aside; or one of its fixes edits a
// file that is not saved's own. Such a package is analyzed again whenever anything below it changes, as without the
// early cutoff, since its importers' outputs say nothing about where their declarations are.
func positionDependent(saved *packages.Package, analyses map[string]storedAnalysis, knownFiles map[string]bool) bool {
	own := map[string]bool{}
	for _, files := range [][]string{saved.GoFiles, saved.CompiledGoFiles, saved.OtherFiles, saved.EmbedFiles,
		saved.IgnoredFiles} {
		for _, file := range files {
			own[file] = true
		}
	}
	for _, stored := range analyses {
		for _, finding := range stored.Findings {
			for _, named := range namedFiles(finding) {
				if knownFiles[named] && !own[named] {
					return true
				}
			}
			for _, suggestion := range finding.Suggestions {
				for _, edit := range suggestion.Edits {
					if !own[edit.File] {
						return true
					}
				}
			}
		}
	}
	return false
}

// namedFiles are the paths a finding's message may name, each word of it with a trailing `:line` or `:line:column`
// taken off, made absolute against the directory of the finding's file when it is relative.
func namedFiles(finding Finding) []string {
	words := strings.FieldsFunc(finding.Message, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune("\"'`()[]{},;", character)
	})
	named := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.TrimSuffix(word, ".")
		for range 2 {
			index := strings.LastIndexByte(word, ':')
			if index < 0 {
				break
			}
			if _, numberError := strconv.Atoi(word[index+1:]); numberError != nil {
				break
			}
			word = word[:index]
		}
		if word == "" {
			continue
		}
		if !filepath.IsAbs(word) {
			word = filepath.Join(filepath.Dir(finding.File), word)
		}
		named = append(named, filepath.Clean(word))
	}
	return named
}
