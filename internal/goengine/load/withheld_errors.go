package load

import (
	"cmp"
	"go/token"
	"slices"
	"strings"
	"sync"
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
// variants report it alike.
type withheldErrors struct {
	lock     sync.Mutex
	seen     map[withheldKey]bool
	counts   map[string]int
	packages map[string]string
}

// withheldKey is what makes two withheld errors the same error.
type withheldKey struct {
	file         string
	line, column int
	message      string
}

func newWithheldErrors() *withheldErrors {
	return &withheldErrors{seen: map[withheldKey]bool{}, counts: map[string]int{}, packages: map[string]string{}}
}

// add withholds one type error of the dependency packagePath at position.
func (withheld *withheldErrors) add(packagePath string, position token.Position, message string) {
	withheld.lock.Lock()
	defer withheld.lock.Unlock()
	key := withheldKey{position.Filename, position.Line, position.Column, message}
	if withheld.seen[key] {
		return
	}
	withheld.seen[key] = true
	withheld.counts[position.Filename]++
	withheld.packages[position.Filename] = packagePath
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
