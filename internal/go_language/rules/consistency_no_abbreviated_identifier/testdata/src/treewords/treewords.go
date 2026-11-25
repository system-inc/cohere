// Package treewords declares abbreviations go/ast, go/types and go/ir taught Go code, beside full words that open
// with the same letters, which the vocabulary must leave alone.
package treewords

import (
	"go/ast"
	"go/token"
	"go/types"
)

// A word with one meaning offers its spelling, as the whole name, a prefix and a segment.
func describe(expr ast.Expr) string { // want `Identifier "expr" should not be abbreviated\. Use "expression" or a more descriptive name\.`
	return types.ExprString(expr)
}

func countConditions(statement *ast.IfStmt) int {
	condExpression := statement.Cond // want `Identifier "condExpression" should not be abbreviated\. Use "conditionExpression"\.`
	if condExpression == nil {
		return 0
	}
	return 1
}

func positionOf(fset *token.FileSet, node ast.Node) token.Position { // want `Identifier "fset" should not be abbreviated\. Use "fileSet" or a more descriptive name\.`
	return fset.Position(node.Pos())
}

type visitorFunc func(node ast.Node) bool // want `Identifier "visitorFunc" abbreviates "Func"\. Use "visitorFunction"\.`

// `typ` would expand to Go's keyword, so the whole word asks for a descriptive name, as `err` does, and offers none.
func kindOf(typ types.Type) string { // want `Identifier "typ" should not be abbreviated\. Use a descriptive name ending in "Type", since "type" is already Go's name for something else\.`
	return typ.String()
}

// A word with several meanings names each and offers none.
func receiverOf(signature *types.Signature) *types.Var {
	recv := signature.Recv() // want `Identifier "recv" should not be abbreviated\. Use "receiver" or "receive", whichever it means, or a more descriptive name\.`
	return recv
}

func operatorOf(binary *ast.BinaryExpr) token.Token {
	op := binary.Op // want `Identifier "op" should not be abbreviated\. Use "operation", "operator" or "operand", whichever it means, or a more descriptive name\.`
	return op
}

// Full words that open with an abbreviation's letters are no abbreviation: each continues in lowercase, so no word
// ends where the abbreviation does.
type Condition struct {
	Channel      chan int
	Change       int
	Function     func()
	Progress     float64
	Selection    string
	Self         *Condition
	Typical      bool
	Address      string
	Openness     int
	Optional     bool
	Expression   string
	Instruction  int
	Successful   bool
	Predicate    func() bool
	Allocated    int
	Anonymous    bool
	Dereferenced bool
	Statement    string
	Pointer      *int
	Receiver     string
}

func selectAll(conditions []Condition) []Condition { return conditions }

func openFile() string { return "" }

func typeName(condition Condition) string { return condition.Address }
