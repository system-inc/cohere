package check

import (
	"go/types"
	"math/bits"

	"golang.org/x/tools/go/packages"
)

// closureIndex is the import closures of one decide's candidates, made once before the decide fans out and only read
// after, so every worker asks it at once without a lock (#nrk4zt0). Of a candidate and a path it answers what a walk of
// the candidate's imports answers: the package of the path a walk of every import reaches (factsHold's declaring
// package), and the types of the one a walk of the imports whose types are set reaches (load.ClosureTypes), without a
// walk per candidate.
//
// A walk keeps the first package of each path it meets and goes on only through that one. A test variant shares its
// path with its plain package, "p [p.test]" with "p", so the index keeps every package of a path (variants) and
// answers, of a candidate, the one its own closure holds (reaches, reachesTyped), never the build's first. When the
// closure holds no two packages of one path, the walk meets every package the candidate reaches, one of each path, so
// the one of the path the closure holds is the one the walk keeps. A candidate whose closure does hold two of a path
// (twoOfAPath) is walked as before, since which the walk keeps turns on the order it ranges Imports in, a map's.
//
// Each package is numbered, every import before its importers, and reaches and reachesTyped hold one row of words per
// package, a bit per package it reaches, the second through imports whose types are set alone, as load.ClosureTypes
// skips an import of no types and never goes on through it. shared marks each package of a path with more than one
// package, and sharedPath numbers those paths, for twoOfAPath.
type closureIndex struct {
	numbers      map[*packages.Package]int
	variants     map[string][]*packages.Package
	words        int
	reaches      []uint64
	reachesTyped []uint64
	shared       []uint64
	sharedPath   []int
	sharedPaths  int

	// cyclic is whether the imports close a cycle, which the load never leaves (load's connect), but a walk would
	// survive, so every candidate is walked as before.
	cyclic bool
}

// newClosureIndex is the index of what candidates reach through their imports.
func newClosureIndex(candidates []*packages.Package) *closureIndex {
	index := &closureIndex{numbers: map[*packages.Package]int{}, variants: map[string][]*packages.Package{}}
	// Numbered in postorder, so each package's imports have their rows before it does; visiting marks a package
	// whose imports are still being numbered, met again only through a cycle.
	var ordered []*packages.Package
	visiting := map[*packages.Package]bool{}
	var number func(current *packages.Package)
	number = func(current *packages.Package) {
		if _, numbered := index.numbers[current]; numbered {
			return
		}
		if visiting[current] {
			index.cyclic = true
			return
		}
		visiting[current] = true
		for _, imported := range current.Imports {
			number(imported)
		}
		delete(visiting, current)
		index.numbers[current] = len(ordered)
		ordered = append(ordered, current)
		index.variants[current.PkgPath] = append(index.variants[current.PkgPath], current)
	}
	for _, candidate := range candidates {
		number(candidate)
	}
	if index.cyclic {
		return index
	}
	index.words = (len(ordered) + 63) / 64
	index.reaches = make([]uint64, len(ordered)*index.words)
	index.reachesTyped = make([]uint64, len(ordered)*index.words)
	for importerNumber, importer := range ordered {
		reaches := index.row(index.reaches, importerNumber)
		reachesTyped := index.row(index.reachesTyped, importerNumber)
		for _, imported := range importer.Imports {
			importedNumber := index.numbers[imported]
			reaches[importedNumber/64] |= 1 << (importedNumber % 64)
			for word, bitsOfWord := range index.row(index.reaches, importedNumber) {
				reaches[word] |= bitsOfWord
			}
			if imported.Types == nil {
				continue
			}
			reachesTyped[importedNumber/64] |= 1 << (importedNumber % 64)
			for word, bitsOfWord := range index.row(index.reachesTyped, importedNumber) {
				reachesTyped[word] |= bitsOfWord
			}
		}
	}
	index.shared = make([]uint64, index.words)
	index.sharedPath = make([]int, len(ordered))
	for _, variants := range index.variants {
		if len(variants) < 2 {
			continue
		}
		for _, variant := range variants {
			variantNumber := index.numbers[variant]
			index.shared[variantNumber/64] |= 1 << (variantNumber % 64)
			index.sharedPath[variantNumber] = index.sharedPaths
		}
		index.sharedPaths++
	}
	return index
}

// row is package number's row of rows, reaches or reachesTyped.
func (index *closureIndex) row(rows []uint64, number int) []uint64 {
	return rows[number*index.words : (number+1)*index.words]
}

// walks is whether candidate's closure is walked as before rather than read from the index: the imports close a
// cycle, or what it reaches, through every import or through those whose types are set, holds two packages of a path.
func (index *closureIndex) walks(candidate *packages.Package) bool {
	if index.cyclic {
		return true
	}
	number := index.numbers[candidate]
	return index.twoOfAPath(index.row(index.reaches, number)) ||
		index.twoOfAPath(index.row(index.reachesTyped, number))
}

// twoOfAPath is whether reached, a row, holds two packages of one path.
func (index *closureIndex) twoOfAPath(reached []uint64) bool {
	seen := make([]uint64, (index.sharedPaths+63)/64)
	for word, bitsOfWord := range reached {
		// Only a package of a path that has more than one can be the second of its path.
		bitsOfWord &= index.shared[word]
		for bitsOfWord != 0 {
			member := word*64 + bits.TrailingZeros64(bitsOfWord)
			bitsOfWord &= bitsOfWord - 1
			path := index.sharedPath[member]
			if seen[path/64]&(1<<(path%64)) != 0 {
				return true
			}
			seen[path/64] |= 1 << (path % 64)
		}
	}
	return false
}

// declaring is the package at path that candidate reaches through its imports, nil for none. It holds only for a
// candidate the index answers, one walks is false of.
func (index *closureIndex) declaring(candidate *packages.Package, path string) *packages.Package {
	reaches := index.row(index.reaches, index.numbers[candidate])
	for _, variant := range index.variants[path] {
		variantNumber := index.numbers[variant]
		if reaches[variantNumber/64]&(1<<(variantNumber%64)) != 0 {
			return variant
		}
	}
	return nil
}

// typesOf is the types of the package at path that candidate reaches through imports whose types are set, nil for
// none, load.ClosureTypes(candidate)[path]. It holds only for a candidate the index answers, one walks is false of.
func (index *closureIndex) typesOf(candidate *packages.Package, path string) *types.Package {
	reachesTyped := index.row(index.reachesTyped, index.numbers[candidate])
	for _, variant := range index.variants[path] {
		variantNumber := index.numbers[variant]
		if reachesTyped[variantNumber/64]&(1<<(variantNumber%64)) != 0 {
			return variant.Types
		}
	}
	return nil
}
