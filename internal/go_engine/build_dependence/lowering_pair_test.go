package build_dependence

import (
	"fmt"
	"go/ast"
	"reflect"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// loweringDifference is the first place one function's two lowerings part: the function, where in it, and what each
// side holds.
type loweringDifference struct {
	function string
	where    string
	host     string
	build    string
}

// loweringPair is the two builds' sides of one comparison.
type loweringPair struct {
	host  *loweringSide
	build *loweringSide
}

// compareFunction walks one function's two lowerings in lockstep: the function's shape, then each block's edges by
// index and each instruction rendered whole, then the expression map, expression by expression in source order. It
// returns the first place they part, and how many instructions the host's holds.
func (pair loweringPair) compareFunction(hosted *intermediate_representation.Function,
	built *intermediate_representation.Function) (loweringDifference, bool, int) {
	name := hosted.RelativeString(nil)
	part := func(where string, host string, build string) (loweringDifference, bool, int) {
		return loweringDifference{function: name, where: where, host: host, build: build}, true, 0
	}
	pair.host.placeFunction(hosted)
	pair.build.placeFunction(built)
	shape := func(side *loweringSide, function *intermediate_representation.Function) string {
		recoverBlock := -1
		if function.Recover != nil {
			recoverBlock = function.Recover.Index
		}
		var parameters []string
		for _, parameter := range function.Parameters {
			parameters = append(parameters, parameter.Name()+" "+typeKey(parameter.Type()))
		}
		for _, free := range function.FreeVariables {
			parameters = append(parameters, "free "+free.Name()+" "+typeKey(free.Type()))
		}
		return fmt.Sprintf("%s %q at %s, signature %s, %s, %d locals, %d blocks, recover %d, %d anonymous",
			side.functionKey(function), function.Synthetic, side.place(function.Pos()), typeKey(function.Signature),
			strings.Join(parameters, "; "), len(function.Locals), len(function.Blocks), recoverBlock,
			len(function.AnonymousFunctions))
	}
	if hostShape, buildShape := shape(pair.host, hosted), shape(pair.build, built); hostShape != buildShape {
		return part("the function", hostShape, buildShape)
	}
	instructions := 0
	for blockIndex, hostedBlock := range hosted.Blocks {
		builtBlock := built.Blocks[blockIndex]
		where := fmt.Sprintf("block %d", blockIndex)
		if hostEdges, buildEdges := blockEdges(hostedBlock), blockEdges(builtBlock); hostEdges != buildEdges {
			return part(where+", its edges", hostEdges, buildEdges)
		}
		if hostedBlock.RangeIndexLoop != builtBlock.RangeIndexLoop {
			return part(where+", its range index loop mark", fmt.Sprint(hostedBlock.RangeIndexLoop),
				fmt.Sprint(builtBlock.RangeIndexLoop))
		}
		if len(hostedBlock.Instructions) != len(builtBlock.Instructions) {
			return part(where+", its instructions", instructionKinds(hostedBlock.Instructions),
				instructionKinds(builtBlock.Instructions))
		}
		for index, hostedInstruction := range hostedBlock.Instructions {
			instructions++
			hostRendering := pair.host.instruction(hostedInstruction)
			buildRendering := pair.build.instruction(builtBlock.Instructions[index])
			if hostRendering != buildRendering {
				return part(fmt.Sprintf("%s, instruction %d", where, index), hostRendering, buildRendering)
			}
		}
	}
	if hostedSyntax, builtSyntax := hosted.Syntax(), built.Syntax(); hostedSyntax != nil && builtSyntax != nil {
		hostExpressions, buildExpressions := expressionsIn(hostedSyntax), expressionsIn(builtSyntax)
		if len(hostExpressions) != len(buildExpressions) {
			return part("the expression map", fmt.Sprintf("%d expressions", len(hostExpressions)),
				fmt.Sprintf("%d expressions", len(buildExpressions)))
		}
		for index, expression := range hostExpressions {
			hostValue, hostIsAddress := hosted.ValueForExpression(expression)
			buildValue, buildIsAddress := built.ValueForExpression(buildExpressions[index])
			hostRendering := fmt.Sprintf("%s: %s address %t", pair.host.place(expression.Pos()),
				pair.host.operand(hostValue), hostIsAddress)
			buildRendering := fmt.Sprintf("%s: %s address %t", pair.build.place(buildExpressions[index].Pos()),
				pair.build.operand(buildValue), buildIsAddress)
			if hostRendering != buildRendering {
				return part("the expression map", hostRendering, buildRendering)
			}
		}
	}
	return loweringDifference{}, false, instructions
}

// expressionsIn are the expressions under syntax, in source order.
func expressionsIn(syntax ast.Node) []ast.Expr {
	var expressions []ast.Expr
	ast.Inspect(syntax, func(node ast.Node) bool {
		if expression, isExpression := node.(ast.Expr); isExpression {
			expressions = append(expressions, expression)
		}
		return true
	})
	return expressions
}

// blockEdges are a block's predecessors and successors, by index.
func blockEdges(block *intermediate_representation.BasicBlock) string {
	var predecessors []int
	var successors []int
	for _, predecessor := range block.Predecessors {
		predecessors = append(predecessors, predecessor.Index)
	}
	for _, successor := range block.Successors {
		successors = append(successors, successor.Index)
	}
	return fmt.Sprintf("predecessors %v, successors %v", predecessors, successors)
}

// instructionKinds are instructions by kind.
func instructionKinds(instructions []intermediate_representation.Instruction) string {
	kinds := make([]string, 0, len(instructions))
	for _, instruction := range instructions {
		kinds = append(kinds, reflect.TypeOf(instruction).Elem().Name())
	}
	return strings.Join(kinds, " ")
}
