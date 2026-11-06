package load

import (
	"cmp"
	"go/token"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// Withheld is the type errors withheld in one file of a dependency the cgo tool would translate.
//
// The load runs no cgo tool, so the type checker fakes package "C" and every C name is of an invalid type. In a
// package that wraps its C calls in `_C_` functions, as the standard library's net and os/user do on linux, a
// caller of a wrapper then reads an invalid type and the checker says so, in a plain file. That is not the
// dependency's fault and not the project's to fix, so a type error in a package outside the workspace whose
// expression reaches the package's cgo files (see cgoReach) is no diagnostic. It is counted here instead, so the
// types record shows what the load could not check. A parse or list error is never withheld, nor a type error that
// reaches no cgo file, nor anything in the project's own packages. A withheld error does not make its package
// ill-typed either, so the rules still run over it for the facts the project's packages read, as they did when
// cgo translated it.
type Withheld struct {
	// Package is the dependency's import path.
	Package string

	// File is absolute.
	File string

	// Errors counts the distinct type errors withheld in File.
	Errors int
}

// withheldErrors gathers the withheld type errors of every package, each once though a package and its test
// variants report it alike, and each package's own, which the findings cache keeps with the package.
type withheldErrors struct {
	lock     sync.Mutex
	seen     map[withheldKey]bool
	counts   map[string]int
	packages map[string]string
	owned    map[*packages.Package][]WithheldError
}

// withheldKey is what makes two withheld errors the same error.
type withheldKey struct {
	file         string
	line, column int
	message      string
}

func newWithheldErrors() *withheldErrors {
	return &withheldErrors{
		seen:     map[withheldKey]bool{},
		counts:   map[string]int{},
		packages: map[string]string{},
		owned:    map[*packages.Package][]WithheldError{},
	}
}

// add withholds one type error of the dependency owner at position.
func (withheld *withheldErrors) add(owner *packages.Package, position token.Position, message string) {
	withheld.lock.Lock()
	defer withheld.lock.Unlock()
	withheld.owned[owner] = append(withheld.owned[owner], WithheldError{
		File: position.Filename, Line: position.Line, Column: position.Column, Message: message,
	})
	packagePath := owner.PkgPath
	key := withheldKey{position.Filename, position.Line, position.Column, message}
	if withheld.seen[key] {
		return
	}
	withheld.seen[key] = true
	withheld.counts[position.Filename]++
	withheld.packages[position.Filename] = packagePath
}

// restore withholds again what a package the findings cache restores withheld when it was checked.
func (withheld *withheldErrors) restore(owner *packages.Package, restoredErrors []WithheldError) {
	for _, restored := range restoredErrors {
		withheld.add(owner, token.Position{Filename: restored.File, Line: restored.Line, Column: restored.Column},
			restored.Message)
	}
}

// ownedBy is the errors withheld in one package, in the order they were withheld.
func (withheld *withheldErrors) ownedBy(owner *packages.Package) []WithheldError {
	withheld.lock.Lock()
	defer withheld.lock.Unlock()
	return withheld.owned[owner]
}

// sorted is every file with a withheld error, sorted by file, and never nil.
func (withheld *withheldErrors) sorted() []Withheld {
	withheld.lock.Lock()
	defer withheld.lock.Unlock()
	result := []Withheld{}
	for file, count := range withheld.counts {
		result = append(result, Withheld{Package: withheld.packages[file], File: file, Errors: count})
	}
	slices.SortFunc(result, func(left, right Withheld) int {
		return cmp.Or(strings.Compare(left.File, right.File), strings.Compare(left.Package, right.Package))
	})
	return result
}
