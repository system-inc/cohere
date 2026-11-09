package canonical_function_hash

import (
	"fmt"
	"sort"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// unsupportedInstruction is a panic value: an instruction the hash does not model, raised from deep in a value's
// digest and recovered where the function is hashed, which refuses the function by the instruction's name.
type unsupportedInstruction struct {
	instruction string
}

// blockChain is a block's effect chain: prefixes[i] is the chain before the block's instruction i.
type blockChain struct {
	prefixes  []duplicate_functions.Digest
	extending bool
}

// functionHasher hashes one function.
type functionHasher struct {
	packageHasher *packageHasher
	function      *intermediate_representation.Function

	blockOrder       []*intermediate_representation.BasicBlock
	blockPosition    map[*intermediate_representation.BasicBlock]int
	parameterIndex   map[*intermediate_representation.Parameter]int
	freeVariable     map[*intermediate_representation.FreeVariable]int
	instructionIndex map[intermediate_representation.Instruction]int
	phis             []*intermediate_representation.Phi
	phiIdentity      map[*intermediate_representation.Phi]duplicate_functions.Digest

	// valueDigests and chains are memos for one pass over the function; each phi refinement round resets them,
	// since every digest downstream of a phi changes with the phi's name.
	valueDigests map[intermediate_representation.Value]duplicate_functions.Digest
	chains       map[*intermediate_representation.BasicBlock]*blockChain
}

func newFunctionHasher(packageHasher *packageHasher, function *intermediate_representation.Function) *functionHasher {
	hasher := &functionHasher{
		packageHasher:    packageHasher,
		function:         function,
		blockPosition:    map[*intermediate_representation.BasicBlock]int{},
		parameterIndex:   map[*intermediate_representation.Parameter]int{},
		freeVariable:     map[*intermediate_representation.FreeVariable]int{},
		instructionIndex: map[intermediate_representation.Instruction]int{},
		phiIdentity:      map[*intermediate_representation.Phi]duplicate_functions.Digest{},
	}
	hasher.blockOrder = canonicalBlockOrder(function)
	for position, block := range hasher.blockOrder {
		hasher.blockPosition[block] = position
		for index, instruction := range block.Instructions {
			hasher.instructionIndex[instruction] = index
			if phi, isPhi := instruction.(*intermediate_representation.Phi); isPhi {
				hasher.phis = append(hasher.phis, phi)
			}
		}
	}
	for index, parameter := range function.Parameters {
		hasher.parameterIndex[parameter] = index
	}
	for index, freeVariable := range function.FreeVariables {
		hasher.freeVariable[freeVariable] = index
	}
	return hasher
}

func (hasher *functionHasher) resetMemos() {
	hasher.valueDigests = map[intermediate_representation.Value]duplicate_functions.Digest{}
	hasher.chains = map[*intermediate_representation.BasicBlock]*blockChain{}
}

// chainBefore is the effect chain in front of an instruction in its block.
func (hasher *functionHasher) chainBefore(
	instruction intermediate_representation.Instruction,
) duplicate_functions.Digest {
	block := instruction.Block()
	index := hasher.instructionIndex[instruction]
	chain := hasher.chains[block]
	if chain == nil {
		start := duplicate_functions.NewDigestBuilder("block-start").Number(hasher.blockPosition[block]).Sum()
		chain = &blockChain{prefixes: []duplicate_functions.Digest{start}}
		hasher.chains[block] = chain
	}
	if index < len(chain.prefixes) {
		return chain.prefixes[index]
	}
	if chain.extending {
		// Invariant: an operand comes before its user in a block, so a chain never needs itself; reaching here is a
		// builder shape this hash misread, and refusing the function beats a silently wrong digest.
		panic(unsupportedInstruction{instruction: fmt.Sprintf("an effect chain of block %d needed past itself",
			block.Index)})
	}
	chain.extending = true
	for len(chain.prefixes) <= index {
		current := len(chain.prefixes) - 1
		next := chain.prefixes[current]
		if isEffect(block.Instructions[current]) {
			next = duplicate_functions.NewDigestBuilder("chain").Digest(next).
				Digest(hasher.instructionDigest(block.Instructions[current])).Sum()
		}
		chain.prefixes = append(chain.prefixes, next)
	}
	chain.extending = false
	return chain.prefixes[index]
}

// chainAfter is a block's whole effect chain.
func (hasher *functionHasher) chainAfter(block *intermediate_representation.BasicBlock) duplicate_functions.Digest {
	last := block.Instructions[len(block.Instructions)-1]
	before := hasher.chainBefore(last)
	if !isEffect(last) {
		return before
	}
	return duplicate_functions.NewDigestBuilder("chain").Digest(before).Digest(hasher.instructionDigest(last)).Sum()
}

// instructionDigest is an instruction's digest: a value's own, or for an instruction with no value, its kind, payload
// and operands.
func (hasher *functionHasher) instructionDigest(
	instruction intermediate_representation.Instruction,
) duplicate_functions.Digest {
	if value, isValue := instruction.(intermediate_representation.Value); isValue {
		return hasher.valueDigest(value)
	}
	builder := duplicate_functions.NewDigestBuilder(fmt.Sprintf("%T", instruction))
	hasher.writePayload(builder, instruction)
	hasher.writeOperands(builder, instruction)
	return builder.Sum()
}

func (hasher *functionHasher) writeOperands(
	builder *duplicate_functions.DigestBuilder,
	instruction intermediate_representation.Instruction,
) {
	for _, operand := range instruction.Operands(nil) {
		if *operand == nil {
			builder.Text("nil")
			continue
		}
		builder.Digest(hasher.valueDigest(*operand))
	}
}

// writePayload writes what an instruction carries besides its operands and its result type.
func (hasher *functionHasher) writePayload(
	builder *duplicate_functions.DigestBuilder,
	instruction intermediate_representation.Instruction,
) {
	switch typed := instruction.(type) {
	case *intermediate_representation.BinaryOperation:
		builder.Text(typed.Operator.String()).Text(typeString(typed.X.Type()))
	case *intermediate_representation.UnaryOperation:
		builder.Text(typed.Operator.String())
	case *intermediate_representation.Call:
		writeCallPayload(builder, &typed.Call)
	case *intermediate_representation.Go:
		writeCallPayload(builder, &typed.Call)
	case *intermediate_representation.Defer:
		writeCallPayload(builder, &typed.Call)
	case *intermediate_representation.Allocation:
		builder.Flag(typed.Heap)
	case *intermediate_representation.Field:
		builder.Number(typed.Field).Text(fieldName(typed.X.Type(), typed.Field, false))
	case *intermediate_representation.FieldAddress:
		builder.Number(typed.Field).Text(fieldName(typed.X.Type(), typed.Field, true))
	case *intermediate_representation.Extract:
		builder.Number(typed.Index)
	case *intermediate_representation.MapLookup:
		builder.Flag(typed.CommaOk)
	case *intermediate_representation.TypeAssert:
		builder.Text(typeString(typed.AssertedType)).Flag(typed.CommaOk)
	case *intermediate_representation.Receive:
		builder.Flag(typed.CommaOk)
	case *intermediate_representation.Next:
		builder.Flag(typed.IsString)
	case *intermediate_representation.Select:
		builder.Flag(typed.Blocking)
		for _, state := range typed.States {
			builder.Number(int(state.Direction))
		}
	case *intermediate_representation.TypeSwitch:
		for _, condition := range typed.Conditions {
			builder.Text(typeString(condition))
		}
	case *intermediate_representation.CompositeValue:
		builder.Text(typed.Bitmap.String())
	case *intermediate_representation.Slice, *intermediate_representation.MultiConvert,
		*intermediate_representation.Convert, *intermediate_representation.ChangeType,
		*intermediate_representation.ChangeInterface, *intermediate_representation.SliceToArrayPointer,
		*intermediate_representation.SliceToArray, *intermediate_representation.MakeInterface,
		*intermediate_representation.MakeClosure, *intermediate_representation.MakeMap,
		*intermediate_representation.MakeChannel, *intermediate_representation.MakeSlice,
		*intermediate_representation.Index, *intermediate_representation.IndexAddress,
		*intermediate_representation.StringLookup, *intermediate_representation.Range,
		*intermediate_representation.Load, *intermediate_representation.Store, *intermediate_representation.MapUpdate,
		*intermediate_representation.Send, *intermediate_representation.Panic, *intermediate_representation.Return,
		*intermediate_representation.RunDefers, *intermediate_representation.If, *intermediate_representation.Jump,
		*intermediate_representation.Unreachable, *intermediate_representation.ConstantSwitch,
		*intermediate_representation.BlankStore:
		// The operands and the result type already say everything these carry; a Slice's operands spell which of
		// Low, High and Max are nil.
	default:
		// Invariant: this panic never leaves the package. hashFunction recovers it and refuses the function by the
		// instruction's name, since a digest that guessed past an instruction could join two different functions.
		panic(unsupportedInstruction{instruction: fmt.Sprintf("%T", instruction)})
	}
}

func writeCallPayload(builder *duplicate_functions.DigestBuilder, call *intermediate_representation.CallCommon) {
	if call.Method != nil {
		builder.Text("invoke " + call.Method.FullName())
		return
	}
	builder.Text("call")
}

// valueDigest is a value's digest, memoized for the current pass.
func (hasher *functionHasher) valueDigest(value intermediate_representation.Value) duplicate_functions.Digest {
	if known, isKnown := hasher.valueDigests[value]; isKnown {
		return known
	}
	result := hasher.computeValueDigest(value)
	hasher.valueDigests[value] = result
	return result
}

func (hasher *functionHasher) computeValueDigest(value intermediate_representation.Value) duplicate_functions.Digest {
	switch typed := value.(type) {
	case *intermediate_representation.Constant:
		builder := duplicate_functions.NewDigestBuilder("const").Text(typeString(typed.Type()))
		// A zero value with no go/constant form, a nil or an array's or struct's, is one constant per type, where
		// go/ir spelled an array's or struct's out element by element as an AggregateConst.
		if typed.Value == nil {
			return builder.Text("zero").Sum()
		}
		// The constant's value, never its spelling: 16 and 0x10 are one constant.
		return builder.Text(typed.Value.Kind().String()).Text(typed.Value.ExactString()).Sum()
	case *intermediate_representation.Parameter:
		return duplicate_functions.NewDigestBuilder("parameter").Number(hasher.parameterIndex[typed]).
			Text(typeString(typed.Type())).Sum()
	case *intermediate_representation.FreeVariable:
		return duplicate_functions.NewDigestBuilder("free-variable").Number(hasher.freeVariable[typed]).
			Text(typeString(typed.Type())).Sum()
	case *intermediate_representation.Global:
		return duplicate_functions.NewDigestBuilder("global").Text(typed.RelativeString(nil)).Sum()
	case *intermediate_representation.Builtin:
		return duplicate_functions.NewDigestBuilder("builtin").Text(typed.Name()).Text(typeString(typed.Type())).Sum()
	case *intermediate_representation.Function:
		return hasher.functionReference(typed)
	case *intermediate_representation.Phi:
		return hasher.phiIdentity[typed]
	}

	instruction, isInstruction := value.(intermediate_representation.Instruction)
	if !isInstruction {
		// Invariant: this panic never leaves the package. hashFunction recovers it and refuses the function by the
		// value's kind.
		panic(unsupportedInstruction{instruction: fmt.Sprintf("value %T", value)})
	}
	builder := duplicate_functions.NewDigestBuilder(fmt.Sprintf("%T", value)).Text(typeString(value.Type()))
	hasher.writePayload(builder, instruction)
	if binary, isBinary := value.(*intermediate_representation.BinaryOperation); isBinary && isCommutative(binary) {
		left := hasher.valueDigest(binary.X)
		right := hasher.valueDigest(binary.Y)
		if right.Less(left) {
			left, right = right, left
		}
		builder.Digest(left).Digest(right)
	} else {
		hasher.writeOperands(builder, instruction)
	}
	// An effect's value, and a read of memory, is placed by the chain in front of it: two calls of one function are
	// two values, and a load before a store is not the load after it.
	if isEffect(instruction) || readsMemory(value) {
		builder.Digest(hasher.chainBefore(instruction))
	}
	return builder.Sum()
}

// functionReference names a function used as a value or callee: a closure made here by its own canonical digest, any
// other function by its fully qualified name (package path, receiver, name, type arguments).
func (hasher *functionHasher) functionReference(
	function *intermediate_representation.Function,
) duplicate_functions.Digest {
	if function.Parent() != nil && function.Synthetic == "" && function.Blocks != nil {
		return duplicate_functions.NewDigestBuilder("closure").Digest(
			hasher.packageHasher.closureDigest(function),
		).Sum()
	}
	return duplicate_functions.NewDigestBuilder("function").Text(function.RelativeString(nil)).Sum()
}

// refinePhis names each phi by its block and type, then repeatedly by its previous name and its incoming edges, sorted
// by the predecessor's canonical position, until the names stop splitting the phis into more classes. Every cycle in
// the value graph passes through a phi, so each round's hashing is acyclic.
func (hasher *functionHasher) refinePhis() {
	for _, phi := range hasher.phis {
		hasher.phiIdentity[phi] = duplicate_functions.NewDigestBuilder("phi").
			Number(hasher.blockPosition[phi.Block()]).Text(typeString(phi.Type())).Sum()
	}
	if len(hasher.phis) == 0 {
		return
	}
	type edge struct {
		position int
		value    duplicate_functions.Digest
	}
	for round := 0; round <= len(hasher.phis)+1; round++ {
		hasher.resetMemos()
		next := map[*intermediate_representation.Phi]duplicate_functions.Digest{}
		for _, phi := range hasher.phis {
			edges := make([]edge, len(phi.Edges))
			for index, incoming := range phi.Edges {
				edges[index] = edge{position: hasher.blockPosition[phi.Block().Predecessors[index]],
					value: hasher.valueDigest(incoming)}
			}
			sort.Slice(edges, func(left, right int) bool { return edges[left].position < edges[right].position })
			builder := duplicate_functions.NewDigestBuilder("phi-round").Digest(hasher.phiIdentity[phi])
			for _, incoming := range edges {
				builder.Number(incoming.position).Digest(incoming.value)
			}
			next[phi] = builder.Sum()
		}
		stable := distinctCount(next) == distinctCount(hasher.phiIdentity)
		hasher.phiIdentity = next
		if stable {
			return
		}
	}
}

func distinctCount(identities map[*intermediate_representation.Phi]duplicate_functions.Digest) int {
	distinct := map[duplicate_functions.Digest]bool{}
	for _, identity := range identities {
		distinct[identity] = true
	}
	return len(distinct)
}

// functionDigest is the function's digest: its signature's types, then each block in canonical order by its position,
// its effect chain and its terminator.
func (hasher *functionHasher) functionDigest() duplicate_functions.Digest {
	hasher.refinePhis()
	hasher.resetMemos()
	builder := duplicate_functions.NewDigestBuilder("function")
	for _, parameter := range hasher.function.Parameters {
		builder.Text(typeString(parameter.Type()))
	}
	builder.Text("results").Text(typeString(hasher.function.Signature.Results())).
		Flag(hasher.function.Signature.Variadic()).Text("free variables")
	for _, freeVariable := range hasher.function.FreeVariables {
		builder.Text(typeString(freeVariable.Type()))
	}
	for position, block := range hasher.blockOrder {
		builder.Number(position)
		if len(block.Instructions) == 0 {
			builder.Text("empty")
			continue
		}
		builder.Digest(hasher.chainAfter(block))
		terminator := block.Instructions[len(block.Instructions)-1]
		builder.Text(fmt.Sprintf("%T", terminator))
		switch typed := terminator.(type) {
		case *intermediate_representation.If:
			builder.Digest(hasher.valueDigest(typed.Condition))
		case *intermediate_representation.ConstantSwitch:
			hasher.writeOperands(builder, typed)
		}
		for _, successor := range block.Successors {
			builder.Number(hasher.blockPosition[successor])
		}
	}
	return builder.Sum()
}
