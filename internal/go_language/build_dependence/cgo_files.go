package build_dependence

import (
	"go/ast"
	"slices"

	"golang.org/x/tools/go/packages"
)

// A package with cgo files is loaded one of two ways. Translated, its syntax is its plain Go files and the cgo tool's
// outputs in the build cache: each cgo file's translation, whose units a reader sees in the cgo file as written through
// the `//line` directive the translation opens with, and the file the tool generates, the C types, constants and
// functions the cgo files name. The tool writes that file's declarations with no directive, so a reader sees them in
// the cache, but inside the wrapper of an exported function whose result may hold a pointer it writes a `//line`
// naming the cgo file as written (cmd/cgo's writeExports), and a reader sees every unit after one in the cgo file, at
// the generated file's offsets. Or checked as written, with package "C" faked, where every name of C has no type.
//
// Either way a unit in none of the package's plain Go files hangs on the C side, which a build's tags and flags can
// move with every Go file byte for byte shared: `#cgo sometag CFLAGS: -DWIDE` gives a macro another value or a struct
// another layout, a name of C is a constant in one build and a variable in the other, or one build translates the
// package where the other fakes it. No file list says so, a file being matched by its name and never by its bytes, and
// the tool's outputs being in no list at all. So each such unit carries a mark (unitRecord.fromCgo), set as its package
// is added from its live load, and wherever a build's package differs at all, a file only one side compiles or a cgo
// tool that translated it on either side (KeepsCgo), every marked unit is a seed in that build (judgment.seedFiles).
// The mark is the record's own, never a match on a path in the build cache, which a record read back from a store may
// hold stale, and it is set from the parsed file's own name (Graph.addPackage), never from the place a directive gives
// a unit.
//
// In a package checked as written the plain Go files' units hang on the C side too, and carry the mark with the rest
// (fakesCgo): cohere-go's load empties every function body and every var initializer that reaches cgo there, in any
// file of the package, before the graph collects it (load.withholdCgoDeclarations), so such a unit reaches the
// collector with no read and no edge to tie it to a cgo file's unit, and a build that translates the package, or has
// cgo off and compiles a `!cgo` file in the cgo file's place, as every other platform's does, checks its body, or
// types its value, for the first time. So every unit of such a package is marked, the larger set, seeded wherever the
// package differs.
//
// What the load withheld is a seed in every build besides, whatever the difference says (unitRecord.withheld,
// judgment.seedWithheld): the load hands the graph the position of each declaration it withheld from
// (load.PackageWithheld.Declarations), and the graph marks its unit. The load withholds not only what reaches the
// package's own cgo files but whatever holds an invalid expression (load's holdsInvalid), one typed from another faked
// package among them, so a package whose every file is the same in both builds, and which is in no difference, can
// have a unit withheld on the host that a build checks for the first time, a tag having moved what it reads of the
// other package out of that package's cgo file.

// CgoFiles are the files of loaded as written that the cgo tool translated: those of its GoFiles it does not compile
// as written, the tool's outputs compiled in their place (CompiledGoFiles). A package with no cgo file has none, nor
// has one whose build leaves its cgo files out, cgo being off there, or checks them as written with package "C" faked.
func CgoFiles(loaded *packages.Package) []string {
	translated := slices.ContainsFunc(loaded.CompiledGoFiles, func(file string) bool {
		return !slices.Contains(loaded.GoFiles, file)
	})
	if !translated {
		return nil
	}
	var cgoFiles []string
	for _, file := range loaded.GoFiles {
		if !slices.Contains(loaded.CompiledGoFiles, file) {
			cgoFiles = append(cgoFiles, file)
		}
	}
	return cgoFiles
}

// KeepsCgo reports whether a cgo tool translated a package on either side of a build: built is the build's load of it,
// and hostCgoFiles the host's cgo files of it (CgoFiles). One did when the build's translated the package, whatever the
// host did with it, and when the host's did, whatever the build does: translates it again under its own tags and flags,
// checks the cgo files as written, or drops them, cgo being off there or its tags leaving them out. A build that drops
// them has the host's translations as files only the host compiles, seeded as any other, but the file the host's tool
// generated is in no file list, and a plain file may name what it declares, a `_Ctype_` type a `!cgo` file declares
// another way, so its units are seeds there too. Where both builds check the package as written, every name of C is as
// untyped in one as in the other, and cgo is not kept on: a file of the package that differs seeds every marked unit on
// its own (judgment.seedFiles), and a unit the host's load withheld is a seed in every build whatever differs
// (judgment.seedWithheld), so what moves through another faked package is seeded too.
func KeepsCgo(hostCgoFiles []string, built *packages.Package) bool {
	return len(hostCgoFiles) > 0 || len(CgoFiles(built)) > 0
}

// fromCgo reports whether the file of loaded's syntax whose parsed file is named name hangs on the C side: it is an
// output of the cgo tool, which is none of the files as written of a package the tool translated (cgoFiles, loaded's
// CgoFiles), or any file of a package checked as written that has a cgo file (fakesCgo, loaded's).
func fromCgo(loaded *packages.Package, cgoFiles []string, fakesCgo bool, name string) bool {
	return fakesCgo || len(cgoFiles) > 0 && !slices.Contains(loaded.GoFiles, name)
}

// fakesCgo reports whether loaded has a cgo file checked as written among its syntax, one that imports "C", which the
// type checker fakes. A package the cgo tool translated has none: a translation imports no "C".
func fakesCgo(loaded *packages.Package) bool {
	return slices.ContainsFunc(loaded.Syntax, importsC)
}

// importsC reports whether file imports "C", as a cgo file as written does.
func importsC(file *ast.File) bool {
	return slices.ContainsFunc(file.Imports, func(imported *ast.ImportSpec) bool {
		return imported.Path != nil && imported.Path.Value == `"C"`
	})
}
