package rule

import (
	"errors"
	"fmt"
	"go/ast"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
)

// Listener is what one rule hears of a package from the engine's dispatchers, which walk each package once for every
// rule rather than once per rule. A rule declares the callbacks it needs and the kinds each is called for, and is
// called for nothing else. A listener hears one walk, never two: staticcheck's intermediate representation's, the
// engine's own intermediate representation's (IntermediateRepresentation), or the syntax's, and its PackageExit
// follows that walk. A check keeps no state between packages, never recovers, since the
// dispatcher recovers per check per package, and never names its rule, since Context.Report stamps the name it was
// registered under.
type Listener struct {
	// Requires are the analyzers whose results the check reads through Context.ResultOf. One failing on a package
	// fails this rule there and no other on its dispatcher, as it did when the rule walked by itself.
	Requires []*analysis.Analyzer

	// Function is called once for each function of the package's intermediate representation, in the order
	// staticcheck_ir.Functions reads them, before any of the function's instructions.
	Function FunctionCheck
	// Instructions are the kinds Instruction is called for, each written as a typed nil, `(*ir.Call)(nil)`.
	Instructions []ir.Instruction
	// Instruction is called for each instruction of a kind Instructions lists, in block order, after Function.
	Instruction func(context *Context, instruction ir.Instruction)
	// FunctionExit is called once the function's instructions have all been heard.
	FunctionExit func(context *Context, function *ir.Function)

	// IntermediateRepresentation is what the listener hears of the walk over the engine's own intermediate
	// representation, and nil for a listener of another walk. A reader moves from staticcheck's walk to this one by
	// moving its Function, Instructions, Instruction and FunctionExit here, over our types (#g0rwdw1).
	IntermediateRepresentation *IntermediateRepresentationListener

	// Nodes are the kinds Node is called for, each written as a typed nil, `(*ast.CallExpr)(nil)`.
	Nodes []ast.Node
	// Node is called for each node of a kind Nodes lists, in preorder, with the node's ancestors as stack, outermost
	// first, the node itself left out. The walk reuses stack, so a check that keeps it copies it. During the call,
	// Context.Cursor is the node's inspector cursor.
	Node func(context *Context, node ast.Node, stack []ast.Node)
	// FileEnter is called as the syntax walk enters each file.
	FileEnter func(context *Context, file *ast.File)
	// FileExit is called as the syntax walk leaves each file.
	FileExit func(context *Context, file *ast.File)

	// PackageExit is called once the listener's walk has covered the package, for a check that judges what it
	// gathered.
	PackageExit func(context *Context)
}

// IsSyntax is whether the listener hears the syntax walk, declaring Nodes, Node, FileEnter or FileExit.
func (listener *Listener) IsSyntax() bool {
	return len(listener.Nodes) > 0 || listener.Node != nil || listener.FileEnter != nil || listener.FileExit != nil
}

// IsIntermediateRepresentation is whether the listener hears the walk over the engine's own intermediate
// representation, declaring IntermediateRepresentation. A listener that hears neither this walk nor the syntax's
// hears staticcheck's intermediate representation's.
func (listener *Listener) IsIntermediateRepresentation() bool {
	return listener.IntermediateRepresentation != nil
}

// isStaticcheckIr is whether the listener declares anything of the walk over staticcheck's intermediate
// representation: Function, Instructions, Instruction or FunctionExit.
func (listener *Listener) isStaticcheckIr() bool {
	return listener.Function != nil || len(listener.Instructions) > 0 || listener.Instruction != nil ||
		listener.FunctionExit != nil
}

// Validate refuses a listener that would listen for what it does not declare, or declare what nothing calls: one
// with no callback at all, one that declares two walks, one with PackageExit alone, which no walk gathers for, one
// that declares the engine's own intermediate representation's walk with none of its callbacks, kinds listed with no
// callback to call for them, a kind callback with no kinds, a kind that is a bare nil and so names no type, or a kind
// listed twice.
func (listener *Listener) Validate() error {
	declared := []string{}
	if listener.isStaticcheckIr() {
		declared = append(declared, "staticcheck's intermediate representation's walk")
	}
	if listener.IsIntermediateRepresentation() {
		declared = append(declared, "the engine's own intermediate representation's walk")
	}
	if listener.IsSyntax() {
		declared = append(declared, "the syntax walk")
	}
	if len(declared) == 0 {
		if listener.PackageExit != nil {
			return errors.New("declares PackageExit alone, and no walk gathers anything for it to judge")
		}
		return errors.New("declares no callback, so nothing would ever call it")
	}
	if len(declared) > 1 {
		return fmt.Errorf("declares both %s and %s, where a listener hears one; register a listener for each",
			declared[0], declared[1])
	}
	if instructionsError := validateKinds("instruction", "Instructions", "Instruction", listener.Instructions,
		listener.Instruction != nil); instructionsError != nil {
		return instructionsError
	}
	if listener.IsIntermediateRepresentation() {
		return listener.IntermediateRepresentation.validate()
	}
	return validateKinds("node", "Nodes", "Node", listener.Nodes, listener.Node != nil)
}

// validateKinds refuses one kind list against its callback: kinds with no callback, a callback with no kinds, a bare
// nil kind, and a kind listed twice.
func validateKinds[Kind any](noun string, listName string, callbackName string, kinds []Kind, hasCallback bool) error {
	if len(kinds) > 0 && !hasCallback {
		return fmt.Errorf("lists %d %s kinds in %s and has no %s to call for them", len(kinds), noun, listName,
			callbackName)
	}
	if hasCallback && len(kinds) == 0 {
		return fmt.Errorf("sets %s and lists no %s kinds in %s, so it would never be called", callbackName, noun,
			listName)
	}
	seen := map[reflect.Type]bool{}
	for _, kind := range kinds {
		kindType := reflect.TypeOf(kind)
		if kindType == nil {
			return fmt.Errorf("lists a bare nil in %s, which names no %s kind; write a kind as a typed nil", listName,
				noun)
		}
		if seen[kindType] {
			return fmt.Errorf("lists the %s kind %s twice in %s", noun, kindType, listName)
		}
		seen[kindType] = true
	}
	return nil
}
