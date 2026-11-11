package correctness_no_caller_data_mutation

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// ioReader finds io.Reader in the package's import graph the first time a method named Read asks, since most
// packages declare none. It is the rule's state over its package, held in its context.
type ioReader struct {
	looked bool
	// reader is io.Reader's interface, or nil when no package in the import graph is io. A type can then still have
	// the method by shape, and its Read is judged like any other function, a missed exemption rather than a guess.
	reader *types.Interface
}

// bufferOf is the buffer parameter of function when function is a Read method whose receiver type, or a pointer to
// it, implements io.Reader, which makes its signature Read([]byte) (int, error), or nil. A function merely named Read
// implements nothing and has no buffer here.
func (lookup *ioReader) bufferOf(context *rule.Context, function *ast.FuncDecl) *types.Var {
	if function.Recv == nil || function.Name.Name != "Read" {
		return nil
	}
	method, isFunction := context.TypesInfo.Defs[function.Name].(*types.Func)
	if !isFunction || method.Signature().Recv() == nil || method.Signature().Params().Len() != 1 {
		return nil
	}
	if !lookup.looked {
		lookup.looked = true
		lookup.reader = findIoReader(context.Package, context.Reads())
	}
	if lookup.reader == nil {
		return nil
	}
	// A pointer to the receiver's type holds both its value and its pointer methods in its method set.
	receiverType := method.Signature().Recv().Type()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	if !types.Implements(types.NewPointer(receiverType), lookup.reader) {
		return nil
	}
	return method.Signature().Params().At(0)
}
