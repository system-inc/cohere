package intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
	"sync"

	"honnef.co/go/tools/go/types/typeutil"
)

// A Program is a partial or complete Go program converted to IR form.
type Program struct {
	FileSet    *token.FileSet              // position information for the files of this Program
	imported   map[string]*Package         // all importable Packages, keyed by import path
	packages   map[*types.Package]*Package // all created Packages
	MethodSets typeutil.MethodSetCache     // cache of type-checker's method-sets

	canon *canonizer     // type canonicalization map
	ctxt  *types.Context // cache for type checking instantiations

	// objectMethods is a memoization of objectMethod
	// to avoid creation of duplicate methods from type information.
	objectMethodsMutex sync.Mutex
	objectMethods      map[*types.Func]*Function

	noReturn func(*types.Func) bool // (optional) predicate that decides whether a given call cannot return
}

// Build calls Package.Build for each package in prog, in parallel.
//
// Build is intended for whole-program analysis; a typical compiler
// need only build a single package.
//
// Build is idempotent and thread-safe.
func (prog *Program) Build() {
	var waitGroup sync.WaitGroup
	for _, value := range prog.packages {
		waitGroup.Add(1)
		cpuLimit <- unit{} // acquire a token
		go func(irPackage *Package) {
			irPackage.Build()
			waitGroup.Done()
			<-cpuLimit // release a token
		}(value)
	}
	waitGroup.Wait()
}

// CreatePackage creates and returns an IR Package from the
// specified type-checked, error-free file ASTs, and populates its
// Members mapping.
//
// importable determines whether this package should be returned by a
// subsequent call to ImportedPackage(pkg.Path()).
//
// The real work of building IR form for each function is not done
// until a subsequent call to Package.Build.
func (prog *Program) CreatePackage(
	goPackage *types.Package,
	files []*ast.File,
	info *types.Info,
	importable bool,
) *Package {
	if goPackage == nil {
		// Invariant: callers create a package from a *types.Package go/types made, never nil, which would make
		// the scope below types.Universe.
		panic("nil pkg")
	}
	irPackage := &Package{
		Program: prog,
		Members: make(map[string]Member),
		values:  make(map[types.Object]Member),
		Types:   goPackage,
		syntax:  info != nil,
		// transient values (cleared after Package.Build)
		info:        info,
		files:       files,
		initVersion: make(map[ast.Expr]string),
	}

	/* synthesized package initializer */
	irPackage.init = &Function{
		name:      "init",
		Signature: new(types.Signature),
		Synthetic: "package initializer",
		Package:   irPackage,
		Program:   prog,
		build:     (*builder).buildPackageInit,
		info:      irPackage.info,
		goversion: "", // See Package.build for details.
	}
	irPackage.Members[irPackage.init.name] = irPackage.init
	irPackage.Functions = append(irPackage.Functions, irPackage.init)
	irPackage.created = append(irPackage.created, irPackage.init)

	// Allocate all package members: vars, funcs, consts and types.
	if len(files) > 0 {
		// Go source package.
		for _, file := range files {
			goversion := version.Lang(fileGoVersion(irPackage.info, file))
			for _, decl := range file.Decls {
				membersFromDecl(irPackage, decl, goversion)
			}
		}
	} else {
		// GC-compiled binary package (or "unsafe")
		// No code.
		// No position information.
		scope := irPackage.Types.Scope()
		for _, name := range scope.Names() {
			goObject := scope.Lookup(name)
			memberFromObject(irPackage, goObject, nil, "")
			if typeName, ok := goObject.(*types.TypeName); ok {
				// No Unalias: aliases should not duplicate methods.
				if named, ok := typeName.Type().(*types.Named); ok {
					for index, methodCount := 0, named.NumMethods(); index < methodCount; index++ {
						memberFromObject(irPackage, named.Method(index), nil, "")
					}
				}
			}
		}
	}

	// Add initializer guard variable.
	initguard := &Global{
		Package: irPackage,
		name:    "init$guard",
		typ:     types.NewPointer(tBool),
	}
	irPackage.Members[initguard.Name()] = initguard

	if importable {
		prog.imported[irPackage.Types.Path()] = irPackage
	}
	prog.packages[irPackage.Types] = irPackage

	return irPackage
}

// AllPackages returns a new slice containing all packages created by
// prog.CreatePackage in unspecified order.
func (prog *Program) AllPackages() []*Package {
	pkgs := make([]*Package, 0, len(prog.packages))
	for _, irPackage := range prog.packages {
		pkgs = append(pkgs, irPackage)
	}
	return pkgs
}

// ImportedPackage returns the importable Package whose PkgPath
// is path, or nil if no such Package has been created.
//
// A parameter to CreatePackage determines whether a package should be
// considered importable. For example, no import declaration can resolve
// to the ad-hoc main package created by 'go build foo.go'.
//
// The "importable" concept is a weak one, since most packages are
// importable. This function assumes that all
// types.Package.Path values are unique within the ir.Program, which is
// false---yet this function remains very convenient.
// Clients should use (*Program).Package instead where possible.
// IR doesn't really need a string-keyed map of packages.
//
// Furthermore, the graph of packages may contain multiple variants
// (e.g. "p" vs "p as compiled for q.test"), and each has a different
// view of its dependencies.
func (prog *Program) ImportedPackage(path string) *Package {
	return prog.imported[path]
}

// SetNoReturn sets the predicate used when building the ir.Program
// prog that reports whether a given function cannot return.
// This may be used to prune spurious control flow edges
// after (e.g.) log.Fatal, improving the precision of analyses.
//
// A typical implementation is the [ctrlflow.CFGs.NoReturn] method from
// [golang.org/x/tools/go/analysis/passes/ctrlflow].
func (prog *Program) SetNoReturn(predicate func(*types.Func) bool) {
	prog.noReturn = predicate
}

// NewFunction returns a new synthetic Function instance belonging to
// prog, with its name and signature fields set as specified.
//
// The caller is responsible for initializing the remaining fields of
// the function object, e.g. Package, Parameters, Blocks.
//
// It is practically impossible for clients to construct well-formed
// IR functions/packages/programs directly, so we assume this is the
// job of the Builder alone.  NewFunction exists to provide clients a
// little flexibility.  For example, analysis tools may wish to
// construct fake Functions for the root of the callgraph, a fake
// "reflect" package, etc.
//
// The shape of this API is provisional.
func (prog *Program) NewFunction(name string, sig *types.Signature, provenance string) *Function {
	return &Function{Program: prog, name: name, Signature: sig, Synthetic: provenance}
}

// Package returns the IR Package corresponding to the specified
// type-checker package object.
// It returns nil if no such IR package has been created.
func (prog *Program) Package(object *types.Package) *Package {
	return prog.packages[object]
}

// packageLevelMember returns the package-level member corresponding
// to the specified symbol, which may be a package-level const
// (*NamedConstant), var (*Global) or func/method (*Function) of some
// package in prog.
//
// It returns nil if the object belongs to a package that has not been
// created by prog.CreatePackage.
func (prog *Program) packageLevelMember(object types.Object) Member {
	if irPackage, ok := prog.packages[object.Pkg()]; ok {
		return irPackage.values[object]
	}
	return nil
}

// FunctionValue returns the SSA function or (non-interface) method
// denoted by the specified func symbol. It returns nil if the symbol
// denotes an interface method, or belongs to a package that was not
// created by prog.CreatePackage.
func (prog *Program) FunctionValue(object *types.Func) *Function {
	function, _ := prog.packageLevelMember(object).(*Function)
	return function
}

// ConstantValue returns the IR Value denoted by the source-level named
// constant obj.
func (prog *Program) ConstantValue(object *types.Const) *Constant {
	// Constants for const objects and constant ast.Exprs are not shared;
	// each call allocates its own.

	// Universal constant? {true,false,nil}
	if object.Parent() == types.Universe {
		return NewConstant(object.Val(), object.Type(), nil)
	}
	// Package-level named constant?
	if member := prog.packageLevelMember(object); member != nil {
		namedConstant, isNamedConstant := member.(*NamedConstant)
		if !isNamedConstant {
			// Invariant: memberFromObject records every package-level *types.Const as a *NamedConstant.
			panic(fmt.Sprintf("member for constant %s is a %T, not a *NamedConstant", object, member))
		}
		return namedConstant.Value
	}
	return NewConstant(object.Val(), object.Type(), nil)
}
