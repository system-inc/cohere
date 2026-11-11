package check

import (
	"bytes"
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// outputDigest is what an importer can read of a package, hashed by value: the digest of its API (exportDigest), then
// every fact it exported, slot by slot in order of slot name, the object facts sorted by objectpath, type and the
// digest of the fact's value (factValueDigest), the package facts by type and value digest. Two runs that give a
// package the same digest give every importer the same types and the same facts, so an importer whose own inputs did
// not move need not be analyzed again (see CacheSession.secondKey). It is never a digest of bytes, since the export
// data's bytes move with how the package's dependencies were loaded and a fact's with what gob encoded before it,
// while what an importer reads did not move (#z14ch7c).
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
func outputDigest(export cache.Key, analyses map[string]analysisFacts) cache.Key {
	parts := [][]byte{[]byte("output"), export[:]}
	for _, slot := range slices.Sorted(maps.Keys(analyses)) {
		facts := analyses[slot]
		objectFacts := slices.Clone(facts.objectFacts)
		slices.SortFunc(objectFacts, func(left, right objectFactDigest) int {
			return cmp.Or(cmp.Compare(left.path, right.path), compareFactDigests(left.fact, right.fact))
		})
		packageFacts := slices.Clone(facts.packageFacts)
		slices.SortFunc(packageFacts, compareFactDigests)
		parts = append(parts, []byte(slot), []byte(strconv.Itoa(len(objectFacts))))
		for _, objectFact := range objectFacts {
			parts = append(parts, []byte(objectFact.path), []byte(objectFact.fact.typeName), objectFact.fact.value[:])
		}
		parts = append(parts, []byte(strconv.Itoa(len(packageFacts))))
		for _, packageFact := range packageFacts {
			parts = append(parts, []byte(packageFact.typeName), packageFact.value[:])
		}
	}
	return cache.DigestOf(parts...)
}

// analysisFacts are the facts one analyzer exported on one package, as outputDigest reads them: by value.
type analysisFacts struct {
	objectFacts  []objectFactDigest
	packageFacts []factDigest
}

// objectFactDigest is a fact about one object of the package, by the object's objectpath, as outputDigest reads it.
type objectFactDigest struct {
	path objectpath.Path
	fact factDigest
}

// factDigest is one fact as outputDigest reads it: its type's name, which factTypeName gives, and the digest of its
// value (factValueDigest).
type factDigest struct {
	typeName string
	value    cache.Key
}

// compareFactDigests orders two facts by their type's name, then by their value's digest.
func compareFactDigests(left factDigest, right factDigest) int {
	return cmp.Or(cmp.Compare(left.typeName, right.typeName), bytes.Compare(left.value[:], right.value[:]))
}

// positionDependent reports whether a finding of saved can change with a line of another package, by naming a file of
// it (see namesForeignFile). Such a package is analyzed again whenever anything below it changes, as without the early
// cutoff, since its importers' outputs say nothing about where their declarations are.
func positionDependent(saved *packages.Package, analyses map[string]storedAnalysis, known *knownFiles) bool {
	own := ownFiles(saved)
	for _, stored := range analyses {
		for _, finding := range stored.Findings {
			if namesForeignFile(finding, own, known) {
				return true
			}
		}
	}
	return false
}

// ownFiles is every file of owner's, of each kind the load lists.
func ownFiles(owner *packages.Package) map[string]bool {
	own := map[string]bool{}
	for _, files := range [][]string{owner.GoFiles, owner.CompiledGoFiles, owner.OtherFiles, owner.EmbedFiles,
		owner.IgnoredFiles} {
		for _, file := range files {
			own[file] = true
		}
	}
	return own
}

// knownFiles are every file of a load by path, and every base name among them, so a word of a finding's message whose
// base name no file has is passed over before it is made a path (namedFiles), which most words are.
type knownFiles struct {
	paths     map[string]bool
	baseNames map[string]bool
}

// newKnownFiles is an empty set of a load's files.
func newKnownFiles() *knownFiles {
	return &knownFiles{paths: map[string]bool{}, baseNames: map[string]bool{}}
}

// addPackage adds every file of owner's, of each kind the load lists (ownFiles), by path and by base name.
//
// Mutates: known gains owner's files
func (known *knownFiles) addPackage(owner *packages.Package) {
	for file := range ownFiles(owner) {
		known.paths[file] = true
		known.baseNames[filepath.Base(file)] = true
	}
}

// namesForeignFile reports whether finding names a file that is not one of own, its package's: its message names a
// file of known, those the load holds, by its absolute path or by its path relative to the finding's own directory, as
// structtag writes it, a trailing `:line` or `:line:column` aside; or one of its fixes edits any file not of own.
func namesForeignFile(finding Finding, own map[string]bool, known *knownFiles) bool {
	for _, named := range namedFiles(finding, known.baseNames) {
		if known.paths[named] && !own[named] {
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
	return false
}

// namedFiles are the paths a finding's message may name of files whose base names are among baseNames, each word of it
// with a trailing `:line` or `:line:column` taken off, made absolute against the directory of the finding's file when
// it is relative. A word whose base name no file has is passed over unjoined: cleaning a path keeps its last element,
// so its path could name no file, except when that element is `.` or `..`, which cleaning folds into the elements
// before it, and such a word is always made a path.
func namedFiles(finding Finding, baseNames map[string]bool) []string {
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
		if base := filepath.Base(word); base != "." && base != ".." && !baseNames[base] {
			continue
		}
		if !filepath.IsAbs(word) {
			word = filepath.Join(filepath.Dir(finding.File), word)
		}
		named = append(named, filepath.Clean(word))
	}
	return named
}
