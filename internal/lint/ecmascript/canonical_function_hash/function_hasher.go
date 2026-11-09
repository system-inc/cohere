package canonical_function_hash

import (
	"fmt"
	"sort"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

// functionHasher hashes one lowered function.
type functionHasher struct {
	file         *fileHasher
	function     *high_level_intermediate_representation.Function
	values       map[static_single_assignment.IdentifierId]duplicate_functions.Digest
	cells        map[static_single_assignment.DeclarationId]duplicate_functions.Digest
	contextIndex map[static_single_assignment.DeclarationId]int
	allocations  map[duplicate_functions.Digest]int
	order        []*high_level_intermediate_representation.BasicBlock
	rpoIndex     map[static_single_assignment.BlockId]int
	groupOf      map[static_single_assignment.BlockId]int
	groups       [][]*high_level_intermediate_representation.BasicBlock
	nullish      map[static_single_assignment.BlockId]bool
	dependencies map[*ast.Node]bool

	// size is the instructions that compute something, nested functions' included: every instruction but the copies
	// (LoadLocal, StoreLocal, a type cast) and the declarations and memoization markers that compute nothing.
	size int
}

func newFunctionHasher(file *fileHasher, function *high_level_intermediate_representation.Function) *functionHasher {
	return &functionHasher{
		file:         file,
		function:     function,
		values:       map[static_single_assignment.IdentifierId]duplicate_functions.Digest{},
		cells:        map[static_single_assignment.DeclarationId]duplicate_functions.Digest{},
		contextIndex: map[static_single_assignment.DeclarationId]int{},
		allocations:  map[duplicate_functions.Digest]int{},
		rpoIndex:     map[static_single_assignment.BlockId]int{},
		groupOf:      map[static_single_assignment.BlockId]int{},
		nullish:      map[static_single_assignment.BlockId]bool{},
		dependencies: map[*ast.Node]bool{},
	}
}

// layout orders the reachable blocks by this hash's own reverse postorder over real edges, then fuses each block
// reached only by a straight-line jump into its predecessor. The IR's own order visits a structural fallthrough
// first, which lays an early return and an else out differently; a walk over real edges alone does not.
func (hasher *functionHasher) layout() {
	visited := map[static_single_assignment.BlockId]bool{}
	var postorder []*high_level_intermediate_representation.BasicBlock
	predecessors := map[static_single_assignment.BlockId]int{}
	var visit func(id static_single_assignment.BlockId)
	visit = func(id static_single_assignment.BlockId) {
		visited[id] = true
		block, found := hasher.function.Block(id)
		if !found {
			return
		}
		high_level_intermediate_representation.EachSuccessor(block.Terminal, func(
			successor static_single_assignment.BlockId,
		) {
			predecessors[successor]++
			if !visited[successor] {
				visit(successor)
			}
		})
		postorder = append(postorder, block)
	}
	visit(hasher.function.Entry)
	for index := len(postorder) - 1; index >= 0; index-- {
		hasher.rpoIndex[postorder[index].Id] = len(hasher.order)
		hasher.order = append(hasher.order, postorder[index])
	}
	fusedNext := map[static_single_assignment.BlockId]*high_level_intermediate_representation.BasicBlock{}
	fused := map[static_single_assignment.BlockId]bool{}
	for _, block := range hasher.order {
		successor, straight := singleSuccessor(block.Terminal)
		if !straight || successor == hasher.function.Entry || predecessors[successor] != 1 {
			continue
		}
		next, found := hasher.function.Block(successor)
		if !found {
			continue
		}
		fusedNext[block.Id] = next
		fused[successor] = true
	}
	for _, block := range hasher.order {
		if fused[block.Id] {
			continue
		}
		position := len(hasher.groups)
		var group []*high_level_intermediate_representation.BasicBlock
		for current := block; current != nil; current = fusedNext[current.Id] {
			group = append(group, current)
			hasher.groupOf[current.Id] = position
		}
		hasher.groups = append(hasher.groups, group)
	}
	for _, block := range hasher.order {
		if logical, isLogical := block.Terminal.(*high_level_intermediate_representation.Logical); isLogical &&
			logical.Operator == "??" {
			hasher.nullish[logical.Fallthrough] = true
		}
	}
}

func (hasher *functionHasher) position(block static_single_assignment.BlockId) (int, error) {
	position, reached := hasher.groupOf[block]
	if !reached {
		return 0, notHashable("a jump to a block nothing reaches")
	}
	return position, nil
}

func (hasher *functionHasher) value(
	place high_level_intermediate_representation.Place,
) (duplicate_functions.Digest, error) {
	if hasher.isCell(place.Identifier) {
		return duplicate_functions.Digest{}, notHashable("a cell read as a value")
	}
	value, defined := hasher.values[place.Identifier]
	if !defined {
		return duplicate_functions.Digest{}, notHashable("a value used before its definition")
	}
	return value, nil
}

func (hasher *functionHasher) set(
	place high_level_intermediate_representation.Place,
	value duplicate_functions.Digest,
) {
	hasher.values[place.Identifier] = value
}

// isCell reports a binding that is memory rather than a value: one this function declares and a closure reassigns,
// which single-assignment construction leaves unversioned, or one this function closes over, which can change between
// reads. Either way its reads hash with the effect chain. Keyed by declaration, so every version is the same cell.
func (hasher *functionHasher) isCell(id static_single_assignment.IdentifierId) bool {
	identifier := hasher.function.Identifier(id)
	if identifier == nil {
		return false
	}
	if _, captured := hasher.contextIndex[identifier.Declaration]; captured {
		return true
	}
	return hasher.function.ContextDeclarations != nil && hasher.function.ContextDeclarations[identifier.Declaration]
}

// cell names a memory binding: a captured one by its position in the context, a declared one by first-seen order.
func (hasher *functionHasher) cell(id static_single_assignment.IdentifierId) duplicate_functions.Digest {
	declaration := hasher.function.Identifier(id).Declaration
	if index, captured := hasher.contextIndex[declaration]; captured {
		return duplicate_functions.NewDigestBuilder("context").Number(index).Sum()
	}
	if cell, named := hasher.cells[declaration]; named {
		return cell
	}
	cell := duplicate_functions.NewDigestBuilder("cell").Number(len(hasher.cells)).Sum()
	hasher.cells[declaration] = cell
	return cell
}

// allocate gives each fresh object an identity: its content, and how many identical allocations came before it in
// walk order, so two `{}` that are later told apart do not hash alike.
func (hasher *functionHasher) allocate(content duplicate_functions.Digest) duplicate_functions.Digest {
	ordinal := hasher.allocations[content]
	hasher.allocations[content] = ordinal + 1
	return duplicate_functions.NewDigestBuilder("allocation").Digest(content).Number(ordinal).Sum()
}

// hashFunction is the function's digest: its signature, then each fused block in order by its phis, its effect chain
// and its terminal.
func (hasher *functionHasher) hashFunction() (duplicate_functions.Digest, error) {
	function := hasher.function
	for index, context := range function.Context {
		hasher.contextIndex[function.IdentifierOf(context).Declaration] = index
	}
	signature := duplicate_functions.NewDigestBuilder("parameters").Number(len(function.Parameters))
	if function.Node != nil && function.Node.Kind == ast.KindConstructor && function.Node.Parent != nil {
		for _, member := range function.Node.Parent.Members() {
			if member.Kind == ast.KindPropertyDeclaration && member.AsPropertyDeclaration().Initializer != nil &&
				!ast.HasSyntacticModifier(member, ast.ModifierFlagsStatic) {
				return duplicate_functions.Digest{}, refuse(duplicate_functions.RefusalFieldInitializers, "")
			}
		}
	}
	if function.Node != nil {
		for _, parameter := range function.Node.Parameters() {
			if parameterError := parameterSignature(
				signature,
				parameter,
				hasher.file.typeChecker,
			); parameterError != nil {
				return duplicate_functions.Digest{}, parameterError
			}
		}
	}
	for index, parameter := range function.Parameters {
		if hasher.isCell(parameter.Identifier) {
			hasher.cells[function.IdentifierOf(
				parameter,
			).Declaration] = duplicate_functions.NewDigestBuilder("parameter-cell").Number(index).Sum()
			continue
		}
		hasher.set(parameter, duplicate_functions.NewDigestBuilder("parameter").Number(index).Sum())
	}
	// Falling off the end returns the Returns place no store reached: undefined.
	hasher.set(function.Returns, primitive(high_level_intermediate_representation.PrimitiveUndefined{}))
	hasher.layout()

	groupDigests := make([]duplicate_functions.Digest, len(hasher.groups))
	chainEnds := make([]duplicate_functions.Digest, len(hasher.groups))
	terminals := make([]duplicate_functions.Digest, len(hasher.groups))
	loopPhis := make([][]loopPhi, len(hasher.groups))
	for position, group := range hasher.groups {
		memory := duplicate_functions.NewDigestBuilder("group-start").Number(position).Sum()
		for _, block := range group {
			for phiIndex, phi := range block.Phis {
				if hasher.isCell(phi.Place.Identifier) {
					continue
				}
				value, carried, phiError := hasher.phi(position, phiIndex, block, phi)
				if phiError != nil {
					return duplicate_functions.Digest{}, phiError
				}
				hasher.set(phi.Place, value)
				if carried != nil {
					loopPhis[position] = append(loopPhis[position], *carried)
				}
			}
			for _, instructionId := range block.Instructions {
				instruction := function.Instruction(instructionId)
				if instructionError := hasher.instruction(instruction, &memory); instructionError != nil {
					return duplicate_functions.Digest{}, fmt.Errorf("%w (in %T)", instructionError, instruction.Value)
				}
			}
		}
		terminal, terminalError := hasher.terminal(group[len(group)-1].Terminal)
		if terminalError != nil {
			return duplicate_functions.Digest{}, fmt.Errorf("%w (in %T)", terminalError, group[len(group)-1].Terminal)
		}
		chainEnds[position] = memory
		terminals[position] = terminal
	}
	for position := range hasher.groups {
		section := duplicate_functions.NewDigestBuilder("group").Number(position)
		finished := make([]duplicate_functions.Digest, 0, len(loopPhis[position]))
		for _, carried := range loopPhis[position] {
			entry := duplicate_functions.NewDigestBuilder("loop-phi").Digest(carried.provisional)
			sort.SliceStable(carried.backEdges, func(left, right int) bool {
				return hasher.groupOf[carried.backEdges[left].Predecessor] <
					hasher.groupOf[carried.backEdges[right].Predecessor]
			})
			for _, operand := range carried.backEdges {
				value, valueError := hasher.value(operand.Place)
				if valueError != nil {
					return duplicate_functions.Digest{}, fmt.Errorf("%w (in a loop phi)", valueError)
				}
				entry.Number(hasher.groupOf[operand.Predecessor]).Digest(value)
			}
			finished = append(finished, entry.Sum())
		}
		sort.Slice(finished, func(left, right int) bool { return finished[left].Less(finished[right]) })
		for _, phi := range finished {
			section.Digest(phi)
		}
		groupDigests[position] = section.Digest(chainEnds[position]).Digest(terminals[position]).Sum()
	}
	result := duplicate_functions.NewDigestBuilder("function").Flag(function.IsAsync).Flag(function.IsGenerator).Digest(
		signature.Sum(),
	).
		Number(len(groupDigests))
	for _, group := range groupDigests {
		result.Digest(group)
	}
	return result.Sum(), nil
}

// phi names one phi of a block. A phi whose operands all come from earlier blocks is named by them, in predecessor
// order. One with a back edge is loop-carried and cannot be named by operands that include itself, so it is named by
// its header and its index there, and its back-edge operands are returned to hash into the header once every block
// is walked. Conservative: two loops that declare their variables in another order hash apart.
func (hasher *functionHasher) phi(
	position int,
	phiIndex int,
	block *high_level_intermediate_representation.BasicBlock,
	phi *high_level_intermediate_representation.Phi,
) (duplicate_functions.Digest, *loopPhi, error) {
	type forwardOperand struct {
		predecessor int
		value       duplicate_functions.Digest
	}
	var forward []forwardOperand
	var backEdges []high_level_intermediate_representation.PhiOperand
	for _, operand := range phi.Operands {
		predecessorIndex, reached := hasher.rpoIndex[operand.Predecessor]
		if !reached {
			continue
		}
		if predecessorIndex >= hasher.rpoIndex[block.Id] {
			backEdges = append(backEdges, operand)
			continue
		}
		value, valueError := hasher.value(operand.Place)
		if valueError != nil {
			return duplicate_functions.Digest{}, nil, fmt.Errorf("%w (in a phi)", valueError)
		}
		forward = append(forward, forwardOperand{hasher.groupOf[operand.Predecessor], value})
	}
	sort.SliceStable(
		forward,
		func(left, right int) bool { return forward[left].predecessor < forward[right].predecessor },
	)
	provisional := duplicate_functions.NewDigestBuilder("phi").Number(position)
	for _, operand := range forward {
		provisional.Number(operand.predecessor).Digest(operand.value)
	}
	if len(backEdges) == 0 {
		return provisional.Sum(), nil, nil
	}
	value := provisional.Text("loop").Number(phiIndex).Sum()
	return value, &loopPhi{provisional: value, backEdges: backEdges}, nil
}

func (hasher *functionHasher) terminal(
	terminal high_level_intermediate_representation.Terminal,
) (duplicate_functions.Digest, error) {
	switch typed := terminal.(type) {
	case *high_level_intermediate_representation.Return:
		value, valueError := hasher.value(typed.Value)
		return duplicate_functions.NewDigestBuilder("return").Digest(value).Sum(), valueError
	case *high_level_intermediate_representation.Throw:
		value, valueError := hasher.value(typed.Value)
		return duplicate_functions.NewDigestBuilder("throw").Digest(value).Sum(), valueError
	case *high_level_intermediate_representation.Unreachable:
		return duplicate_functions.NewDigestBuilder("unreachable").Sum(), nil
	case *high_level_intermediate_representation.If:
		return hasher.branch("truthy", typed.Test, typed.Consequent, typed.Alternate)
	case *high_level_intermediate_representation.Branch:
		kind := "truthy"
		if hasher.nullish[typed.Fallthrough] {
			kind = "nullish"
		}
		return hasher.branch(kind, typed.Test, typed.Consequent, typed.Alternate)
	case *high_level_intermediate_representation.Switch:
		return hasher.switchTerminal(typed)
	case *high_level_intermediate_representation.Try:
		block, blockError := hasher.position(typed.Block)
		if blockError != nil {
			return duplicate_functions.Digest{}, blockError
		}
		result := duplicate_functions.NewDigestBuilder("try").Number(block)
		if high_level_intermediate_representation.HasBlock(typed.Handler) {
			handler, handlerError := hasher.position(typed.Handler)
			if handlerError != nil {
				// A handler nothing reaches: the try body cannot throw into it as lowered.
				result.Text("no-handler")
			} else {
				result.Number(handler)
				if typed.HandlerBinding != nil {
					hasher.set(
						*typed.HandlerBinding,
						duplicate_functions.NewDigestBuilder("caught").Number(handler).Sum(),
					)
				}
			}
		}
		return result.Sum(), nil
	case *high_level_intermediate_representation.MaybeThrow:
		continuation, continuationError := hasher.position(typed.Continuation)
		if continuationError != nil {
			return duplicate_functions.Digest{}, continuationError
		}
		result := duplicate_functions.NewDigestBuilder("maybe-throw").Number(continuation)
		if high_level_intermediate_representation.HasBlock(typed.Handler) {
			handler, handlerError := hasher.position(typed.Handler)
			if handlerError != nil {
				return duplicate_functions.Digest{}, handlerError
			}
			result.Number(handler)
		}
		return result.Sum(), nil
	case *high_level_intermediate_representation.Unsupported:
		return duplicate_functions.Digest{}, refuse(duplicate_functions.RefusalUnsupportedNode, "terminal")
	}
	successor, straight := singleSuccessor(terminal)
	if !straight {
		return duplicate_functions.Digest{}, notHashable(fmt.Sprintf("terminal %T", terminal))
	}
	position, positionError := hasher.position(successor)
	return duplicate_functions.NewDigestBuilder("goto").Number(position).Sum(), positionError
}

func (hasher *functionHasher) switchTerminal(
	typed *high_level_intermediate_representation.Switch,
) (duplicate_functions.Digest, error) {
	test, testError := hasher.value(typed.Test)
	if testError != nil {
		return duplicate_functions.Digest{}, testError
	}
	result := duplicate_functions.NewDigestBuilder("switch").Digest(test)
	hasDefault := false
	for _, switchCase := range typed.Cases {
		if switchCase.Test == nil {
			hasDefault = true
			result.Text("default")
		} else {
			caseValue, caseError := hasher.value(*switchCase.Test)
			if caseError != nil {
				return duplicate_functions.Digest{}, caseError
			}
			result.Digest(caseValue)
		}
		position, positionError := hasher.position(switchCase.Block)
		if positionError != nil {
			return duplicate_functions.Digest{}, positionError
		}
		result.Number(position)
	}
	if !hasDefault && high_level_intermediate_representation.HasBlock(typed.Fallthrough) {
		position, positionError := hasher.position(typed.Fallthrough)
		if positionError != nil {
			return duplicate_functions.Digest{}, positionError
		}
		result.Text("after").Number(position)
	}
	return result.Sum(), nil
}

func (hasher *functionHasher) branch(
	kind string,
	test high_level_intermediate_representation.Place,
	consequent static_single_assignment.BlockId,
	alternate static_single_assignment.BlockId,
) (duplicate_functions.Digest, error) {
	testValue, testError := hasher.value(test)
	if testError != nil {
		return duplicate_functions.Digest{}, testError
	}
	consequentPosition, consequentError := hasher.position(consequent)
	if consequentError != nil {
		return duplicate_functions.Digest{}, consequentError
	}
	alternatePosition, alternateError := hasher.position(alternate)
	if alternateError != nil {
		return duplicate_functions.Digest{}, alternateError
	}
	return duplicate_functions.NewDigestBuilder("branch").Text(kind).Digest(testValue).Number(
		consequentPosition,
	).Number(alternatePosition).Sum(), nil
}

// commutative reports whether an operator's operands may be sorted: equality always, and `+`, `*`, `&`, `|` and `^`
// only when the checker says both sides are numbers, or both bigints, since on anything else each operand's valueOf
// runs in source order and `+` concatenates.
func (hasher *functionHasher) commutative(operator string, node *ast.Node) bool {
	switch operator {
	case "===", "!==", "==", "!=":
		return true
	case "+", "*", "&", "|", "^":
	default:
		return false
	}
	typeChecker := hasher.file.typeChecker
	if node == nil || node.Kind != ast.KindBinaryExpression || typeChecker == nil {
		return false
	}
	expression := node.AsBinaryExpression()
	leftFamily := numericFamily(typeChecker.GetTypeAtLocation(expression.Left))
	rightFamily := numericFamily(typeChecker.GetTypeAtLocation(expression.Right))
	return leftFamily != "" && leftFamily == rightFamily
}
