package line_wrap

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_useless_concatenation"
)

// Shape names the construct a plan breaks, and labels residue.
type Shape string

const (
	ShapeCompositeLiteral Shape = "CompositeLiteral"
	ShapeCallArguments    Shape = "CallArguments"
	ShapeParameters       Shape = "Parameters"
	ShapeResults          Shape = "Results"
	ShapeOperatorChain    Shape = "OperatorChain"
)

// edit replaces source[start:end] with text.
type edit struct {
	start, end int
	text       string
}

type breakKind int

const (
	breakOpen breakKind = iota
	breakBetween
	breakClose
	breakOperator
)

// breakPoint is where a plan starts a new line. The newline lands at offset.
type breakPoint struct {
	offset int
	line   int
	kind   breakKind
	// trailingComma is set on a close break that has to add the comma Go needs before a newline.
	trailingComma bool
}

// listPlan builds the plan for a bracketed list: newline after the opening bracket, after every element,
// and a trailing comma with the closing bracket on its own line.
func (sourceFile *sourceFile) listPlan(
	shape Shape,
	node ast.Node,
	opening, closing token.Pos,
	elements []ast.Node,
) (plan, bool) {
	if len(elements) == 0 || !opening.IsValid() || !closing.IsValid() {
		return plan{}, false
	}
	result := plan{
		shape:    shape,
		start:    sourceFile.offset(node.Pos()),
		end:      sourceFile.offset(node.End()),
		openLine: sourceFile.lineOfPosition(opening),
	}
	first := elements[0]
	if sourceFile.lineOfPosition(opening) == sourceFile.lineOfPosition(first.Pos()) {
		offset := sourceFile.offset(first.Pos())
		result.breaks = append(
			result.breaks,
			breakPoint{offset: offset, line: sourceFile.lineOf(offset), kind: breakOpen},
		)
	}
	for index := 1; index < len(elements); index++ {
		previous := elements[index-1]
		next := elements[index]
		if sourceFile.lineOfPosition(previous.End()) == sourceFile.lineOfPosition(next.Pos()) {
			offset := sourceFile.offset(next.Pos())
			result.breaks = append(
				result.breaks,
				breakPoint{offset: offset, line: sourceFile.lineOf(offset), kind: breakBetween},
			)
		}
	}
	last := elements[len(elements)-1]
	lastEnd := sourceFile.offset(last.End())
	if call, isCall := node.(*ast.CallExpr); isCall && call.Ellipsis.IsValid() {
		lastEnd = sourceFile.offset(call.Ellipsis) + len("...")
	}
	if sourceFile.lineOf(lastEnd) == sourceFile.lineOfPosition(closing) {
		closingOffset := sourceFile.offset(closing)
		if sourceFile.commaBetween(lastEnd, closingOffset) {
			result.breaks = append(
				result.breaks,
				breakPoint{offset: closingOffset, line: sourceFile.lineOf(closingOffset), kind: breakClose},
			)
		} else {
			result.breaks = append(
				result.breaks,
				breakPoint{offset: lastEnd, line: sourceFile.lineOf(lastEnd), kind: breakClose, trailingComma: true},
			)
		}
	}
	if literal, isFunctionLiteral := last.(*ast.FuncLit); isFunctionLiteral {
		result.lastFunctionLiteral =
			sourceFile.lineOfPosition(literal.Pos()) != sourceFile.lineOfPosition(literal.End())
	}
	return result, len(result.breaks) > 0
}

// chainPlan breaks a maximal chain of one operator after each operator, but a + joining two string literals that
// consistency_no_useless_concatenation reports on one line: that rule leaves alone a join across lines, so a break
// there would hide its finding rather than fix the long line. The chain breaks at its other operators instead, and a
// line only that break would cure is left to CuredOnly.
func (sourceFile *sourceFile) chainPlan(root *ast.BinaryExpr) (plan, bool) {
	result := plan{
		shape:    ShapeOperatorChain,
		start:    sourceFile.offset(root.Pos()),
		end:      sourceFile.offset(root.End()),
		openLine: sourceFile.lineOfPosition(root.Pos()),
	}
	var operators []*ast.BinaryExpr
	for current := root; ; {
		operators = append(operators, current)
		left, isBinary := current.X.(*ast.BinaryExpr)
		if !isBinary || left.Op != root.Op {
			break
		}
		current = left
	}
	for index := len(operators) - 1; index >= 0; index-- {
		operator := operators[index]
		if sourceFile.lineOfPosition(operator.OpPos) != sourceFile.lineOfPosition(operator.Y.Pos()) {
			continue
		}
		if _, _, joins := consistency_no_useless_concatenation.JoinedLiterals(operator); joins {
			continue
		}
		offset := sourceFile.offset(operator.OpPos) + len(operator.Op.String())
		result.breaks = append(
			result.breaks,
			breakPoint{offset: offset, line: sourceFile.lineOf(offset), kind: breakOperator},
		)
	}
	return result, len(result.breaks) > 0
}

func nodes[T ast.Node](list []T) []ast.Node {
	result := make([]ast.Node, len(list))
	for index, node := range list {
		result[index] = node
	}
	return result
}

// structuralPlans returns every breakable construct of the file, outermost first.
func (sourceFile *sourceFile) structuralPlans() []plan {
	var plans []plan
	add := func(candidate plan, ok bool) {
		if ok {
			plans = append(plans, candidate)
		}
	}
	var stack []ast.Node
	ast.Inspect(sourceFile.file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, node)
		switch typed := node.(type) {
		case *ast.CompositeLit:
			add(sourceFile.listPlan(ShapeCompositeLiteral, typed, typed.Lbrace, typed.Rbrace, nodes(typed.Elts)))
		case *ast.CallExpr:
			add(sourceFile.listPlan(ShapeCallArguments, typed, typed.Lparen, typed.Rparen, nodes(typed.Args)))
		case *ast.FuncType:
			if typed.Params != nil {
				add(sourceFile.listPlan(
					ShapeParameters,
					typed.Params,
					typed.Params.Opening,
					typed.Params.Closing,
					nodes(typed.Params.List),
				))
			}
			if typed.Results != nil {
				add(sourceFile.listPlan(
					ShapeResults,
					typed.Results,
					typed.Results.Opening,
					typed.Results.Closing,
					nodes(typed.Results.List),
				))
			}
		case *ast.BinaryExpr:
			if typed.Op != token.LAND && typed.Op != token.LOR && typed.Op != token.ADD {
				break
			}
			if parentBinary, isBinary := parent.(*ast.BinaryExpr); isBinary &&
				parentBinary.Op == typed.Op &&
				parentBinary.X == node {
				break
			}
			add(sourceFile.chainPlan(typed))
		}
		return true
	})
	return plans
}

// estimate predicts the widest line that line becomes once the plan is applied. A piece that would start with a
// string literal followed by nothing but closing punctuation counts as fitting, since a long string is left
// whole once it sits alone on its line, and so does a keyed one when Options.ExemptKeyedStrings is set.
func (sourceFile *sourceFile) estimate(candidate plan, line int) int {
	text := sourceFile.lineText(line)
	lineStart, _ := sourceFile.lineBounds(line)
	base := width(indentation(text))
	opensHere := candidate.openLine == line
	widest := 0
	previous := 0
	previousKind := breakKind(-1)
	measure := func(segment string, kind breakKind, extra int) {
		first := lineStart + previous + len(segment) - len(strings.TrimLeft(segment, " \t"))
		if kind >= 0 && (sourceFile.aloneFrom(first, lineStart+previous+len(segment)) ||
			sourceFile.keyedFrom(first, lineStart+previous+len(segment))) {
			return
		}
		var columns int
		switch {
		case kind < 0:
			columns = width(strings.TrimRight(segment, " \t"))
		case kind == breakClose && opensHere:
			columns = base + width(strings.TrimSpace(segment))
		case kind == breakClose:
			columns = max(base-consistency_no_long_line.TabColumns, 0) + width(strings.TrimSpace(segment))
		case opensHere || kind == breakOperator:
			columns = base + consistency_no_long_line.TabColumns + width(strings.TrimSpace(segment))
		default:
			columns = base + width(strings.TrimSpace(segment))
		}
		widest = max(widest, columns+extra)
	}
	for _, point := range candidate.breaks {
		if point.line != line {
			continue
		}
		cut := point.offset - lineStart
		extra := 0
		if point.trailingComma {
			extra = 1
		}
		measure(text[previous:cut], previousKind, extra)
		previous = cut
		previousKind = point.kind
	}
	measure(text[previous:], previousKind, 0)
	return widest
}
