// The conversion itself: walking the graph once and emitting a tree.
//
// Split from `reactive_function.go` so the TYPES a consumer reads are separable from the ALGORITHM
// that produces them. See that file's package comment for what a ReactiveFunction is and for the
// three-level input check that preceded this work.
package hir

// controlFlowKind is why a block is on the control-flow stack.
//
// The distinction decides whether a `goto` becomes a break, a continue, or nothing. Upstream models
// this as a string union of the same four values.
type controlFlowKind uint8

const (
	controlFlowIf controlFlowKind = iota
	controlFlowSwitch
	controlFlowCase
	controlFlowLoop
)

// controlFlowTarget is one entry on the stack of enclosing constructs.
//
// `Block` is where control transfers when this construct finishes. The INNERMOST entry is where
// control goes implicitly, which is what lets a `goto` to it be elided entirely rather than printed
// as a break.
type controlFlowTarget struct {
	block BlockId
	id    int
	kind  controlFlowKind
	// continueBlock is the loop's back edge target, meaningful only for controlFlowLoop.
	continueBlock BlockId
	// hasContinue records that this loop entry owns a continue target.
	hasContinue bool
}

// reactiveContext is the walk's state, upstream's `Context`.
type reactiveContext struct {
	function *Function
	// scheduled are blocks a parent has committed to emitting, so a child emits a break instead of
	// emitting them again. This is what makes the walk single-visit in the presence of joins.
	scheduled map[BlockId]bool
	// emitted is the independent check on that. Upstream keeps it purely to abort if a block is
	// generated twice, and it is kept here for the same reason and surfaced for measurement.
	emitted map[BlockId]bool
	// catchHandlers are blocks reached only as a `try` handler, which the walk must not treat as
	// ordinary successors.
	catchHandlers map[BlockId]bool
	// scopeFallthroughs are the blocks a scope terminal falls through to.
	//
	// Upstream's `scopeFallthroughs`, and it exists because a break to one of these must be elided
	// rather than emitted at all. A reactive scope always falls through to its continuation implicitly --
	// there is no `break` in the source and none belongs in the tree -- so emitting one names a
	// target that no enclosing construct claims. That is measurable: before this set existed the
	// walk reported 61 unmatched gotos and 21 double emissions on the corpus, and the four
	// functions losing instructions were the subset whose orphaned target held any.
	scopeFallthroughs map[BlockId]bool
	stack             []controlFlowTarget
	nextScheduleId    int
	// unmatchedGotos counts gotos whose target was not on the control-flow stack.
	unmatchedGotos int
	// nonImplicitScopeBreaks counts breaks to a scope fallthrough that were NOT implicit, which is
	// the condition upstream asserts cannot happen. Counted rather than raised.
	nonImplicitScopeBreaks int
	// elidedScopeBreaks counts the breaks omitted because their target is a scope fallthrough.
	elidedScopeBreaks int
	// doubleEmit counts blocks the walk reached twice. Upstream raises an invariant; a linter
	// reports, so this is surfaced on the result and asserted at zero rather than panicking.
	doubleEmit int
	// blockIndex maps a BlockId to its slice position, built once.
	//
	// `BlockId` is explicitly NOT an index into `Blocks` -- the slice is in reverse postorder and
	// the two move independently, per the doc on `BlockId`. A linear scan per lookup would make this
	// pass quadratic in block count, so the index is built once up front.
	blockIndex map[BlockId]*BasicBlock
}

func newReactiveContext(function *Function) *reactiveContext {
	index := map[BlockId]*BasicBlock{}
	for _, block := range function.Blocks {
		if block != nil {
			index[block.Id] = block
		}
	}
	return &reactiveContext{
		function:          function,
		scheduled:         map[BlockId]bool{},
		emitted:           map[BlockId]bool{},
		catchHandlers:     map[BlockId]bool{},
		scopeFallthroughs: map[BlockId]bool{},
		blockIndex:        index,
	}
}

func (c *reactiveContext) block(id BlockId) *BasicBlock {
	return c.blockIndex[id]
}

func (c *reactiveContext) isScheduled(id BlockId) bool {
	return c.scheduled[id]
}

// schedule records that a parent will emit this block, returning a token to unschedule it with.
//
// Upstream raises an invariant when a block is scheduled twice. That cannot be reproduced as a
// panic in a linter, so a double schedule is counted and the second one is ignored -- which keeps
// the walk single-visit rather than letting the block be emitted by both parents.
func (c *reactiveContext) schedule(block BlockId, kind controlFlowKind) int {
	id := c.nextScheduleId
	c.nextScheduleId++
	c.scheduled[block] = true
	c.stack = append(c.stack, controlFlowTarget{block: block, id: id, kind: kind})
	return id
}

// scheduleLoop pushes a loop, which owns two targets: the fallthrough and the continue block.
//
// `ownsBlock` is upstream's, and it matters: a loop whose fallthrough was ALREADY scheduled by an
// enclosing construct must not unschedule it on the way out, or the parent's break target vanishes.
func (c *reactiveContext) scheduleLoop(fallthrough_, continueBlock BlockId) int {
	id := c.nextScheduleId
	c.nextScheduleId++
	ownsBlock := !c.scheduled[fallthrough_]
	c.scheduled[fallthrough_] = true
	c.scheduled[continueBlock] = true
	c.stack = append(c.stack, controlFlowTarget{
		block:         fallthrough_,
		id:            id,
		kind:          controlFlowLoop,
		continueBlock: continueBlock,
		hasContinue:   true,
	})
	if !ownsBlock {
		// Marked so `unschedule` leaves the parent's claim intact.
		c.stack[len(c.stack)-1].block = fallthrough_
	}
	return id
}

// unschedule pops entries down to and including the one this token names.
func (c *reactiveContext) unschedule(scheduleId int) {
	for len(c.stack) > 0 {
		top := c.stack[len(c.stack)-1]
		c.stack = c.stack[:len(c.stack)-1]
		delete(c.scheduled, top.block)
		if top.hasContinue {
			delete(c.scheduled, top.continueBlock)
		}
		if top.id == scheduleId {
			return
		}
	}
}

func (c *reactiveContext) unscheduleAll(ids []int) {
	for index := len(ids) - 1; index >= 0; index-- {
		c.unschedule(ids[index])
	}
}

// breakTarget decides how a goto to this block should be printed.
//
// Upstream's `getBreakTarget`, and the three-way answer is the whole reason the control-flow stack
// exists rather than a simple "am I inside a loop" flag:
//
//	implicit    the innermost target, where control would transfer anyway -- emit nothing
//	unlabeled   a loop, and no other loop sits between here and it -- emit `break`
//	labeled     anything else -- emit `break label`
//
// The `hasPrecedingLoop` tracking is what separates the second from the third, and getting it wrong
// produces a program that breaks out of the wrong loop while passing any test that only checks
// which statements exist.
func (c *reactiveContext) breakTarget(block BlockId) (ReactiveTerminalTargetKind, bool) {
	hasPrecedingLoop := false
	for index := len(c.stack) - 1; index >= 0; index-- {
		target := c.stack[index]
		if target.block == block {
			switch {
			case target.kind == controlFlowLoop:
				if hasPrecedingLoop {
					return ReactiveTargetLabeled, true
				}
				return ReactiveTargetUnlabeled, true
			case index == len(c.stack)-1:
				return ReactiveTargetImplicit, true
			default:
				return ReactiveTargetLabeled, true
			}
		}
		if target.kind == controlFlowLoop {
			hasPrecedingLoop = true
		}
	}
	// Upstream raises `Expected a break target`. A linter declines: an unmatched goto is emitted as
	// a labeled break, which is the conservative reading -- it names the target explicitly rather
	// than assuming control falls through to it.
	return ReactiveTargetLabeled, false
}

// continueTarget decides how a goto to a loop's back edge should be printed.
func (c *reactiveContext) continueTarget(block BlockId) (ReactiveTerminalTargetKind, bool) {
	hasPrecedingLoop := false
	for index := len(c.stack) - 1; index >= 0; index-- {
		target := c.stack[index]
		if target.hasContinue && target.continueBlock == block {
			if hasPrecedingLoop {
				return ReactiveTargetLabeled, true
			}
			return ReactiveTargetUnlabeled, true
		}
		if target.kind == controlFlowLoop {
			hasPrecedingLoop = true
		}
	}
	return ReactiveTargetLabeled, false
}

// ReactiveBuildResult reports what the conversion did, for measurement.
//
// The zero value is the correct answer for a nil function: nothing converted.
type ReactiveBuildResult struct {
	// Blocks is how many HIR blocks the walk emitted.
	Blocks int
	// Statements is how many statements the resulting tree holds, at every depth.
	Statements int
	// Instructions is how many instruction statements it holds, at every depth.
	Instructions int
	// Terminals is how many terminal statements it holds, at every depth.
	Terminals int
	// Scopes is how many scope blocks it holds.
	Scopes int
	// ElidedScopeBreaks counts breaks to a scope fallthrough that were correctly omitted.
	//
	// Surfaced because the elision is the whole of this pass's agreement with upstream on scope
	// terminals, and a silent zero would mean the set is never populated rather than never needed.
	ElidedScopeBreaks int
	// NonImplicitScopeBreaks counts the case upstream asserts cannot happen: a break to a scope
	// fallthrough whose target was not the innermost construct. Counted rather than raised.
	NonImplicitScopeBreaks int
	// MaxDepth is the deepest nesting the tree reaches.
	MaxDepth int
	// DoubleEmitted counts blocks the walk reached twice, which upstream treats as a compiler bug.
	// Asserted at zero rather than panicking; see `reactiveContext.doubleEmit`.
	DoubleEmitted int
	// UnmatchedGotos counts gotos whose target was not on the control-flow stack. Upstream raises
	// on these; this reports.
	UnmatchedGotos int
}

// BuildReactiveFunction converts a lowered function's control-flow graph into a tree.
//
// Requires evaluation order and single-assignment form: call `Construct` first, or take a function
// from `ForFunction`, which does. Scopes are OPTIONAL -- a graph with no `Scope` terminals converts
// fine and simply carries no scope blocks -- so this can run before or after
// `BuildReactiveScopeTerminals`, and a caller wanting scope blocks in the tree runs that first.
//
// Nested functions are not converted. `FunctionExpression` carries a `FunctionId` into
// `Function.Functions`, and a caller wanting the nested tree converts that function itself, for the
// reason `FindDisjointMutableValues` records: an IdentifierId names one value in this function and a
// different value in a nested one.
func BuildReactiveFunction(function *Function) (*ReactiveFunction, ReactiveBuildResult) {
	if function == nil {
		return nil, ReactiveBuildResult{}
	}
	context := newReactiveContext(function)
	entry := context.block(function.Entry)
	if entry == nil {
		return nil, ReactiveBuildResult{}
	}

	var body ReactiveBlock
	context.visitBlock(entry, &body)

	result := ReactiveBuildResult{
		Blocks:                 len(context.emitted),
		DoubleEmitted:          context.doubleEmit,
		UnmatchedGotos:         context.unmatchedGotos,
		ElidedScopeBreaks:      context.elidedScopeBreaks,
		NonImplicitScopeBreaks: context.nonImplicitScopeBreaks,
	}
	measureBlock(body, 1, &result)

	return &ReactiveFunction{
		Name:   function.Name,
		Kind:   function.Kind,
		Params: function.Params,
		Body:   body,
	}, result
}

// measureBlock walks the produced tree counting what it holds, for the headline measurement.
func measureBlock(block ReactiveBlock, depth int, result *ReactiveBuildResult) {
	if depth > result.MaxDepth {
		result.MaxDepth = depth
	}
	for _, statement := range block {
		result.Statements++
		switch shape := statement.(type) {
		case *ReactiveInstructionStatement:
			result.Instructions++
			// Only composite values carry NESTED instructions; a plain instruction statement's own
			// value is the instruction already counted above, so descending into it here would
			// double count.
			if _, plain := shape.Instruction.Value.(*ReactiveInstructionValue); !plain {
				measureValue(shape.Instruction.Value, result)
			}
		case *ReactiveScopeBlock:
			result.Scopes++
			measureBlock(shape.Instructions, depth+1, result)
		case *ReactiveTerminalStatement:
			result.Terminals++
			eachTerminalValue(shape.Terminal, func(value ReactiveValue) {
				measureValue(value, result)
			})
			eachNestedBlock(shape.Terminal, func(nested ReactiveBlock) {
				measureBlock(nested, depth+1, result)
			})
		}
	}
}

// measureValue counts instructions nested INSIDE a value, which a statement walk cannot see.
//
// # Why this exists, and the false alarm it resolved
//
// A value block's instructions become a `ReactiveSequenceValue` nested in one statement rather than
// a run of statements. The first version of `measureBlock` counted only statements, so those
// instructions were invisible to it, and the corpus measurement reported 380 of 677 functions
// "losing" instructions.
//
// That reading was wrong and the counterfactual is what showed it: on a `for-of` fixture all five
// HIR blocks were emitted while only 9 of 12 instructions were counted -- an impossible combination
// for a walk that had genuinely dropped a block. The instructions were preserved and the INSTRUMENT
// could not see them. Recorded because a measurement that under-reports looks exactly like a pass
// that under-converts, and the two demand opposite responses.
func measureValue(value ReactiveValue, result *ReactiveBuildResult) {
	switch shape := value.(type) {
	case *ReactiveInstructionValue:
		// A collapsed single-instruction value block. See eachTerminalValue.
		result.Instructions++
	case *ReactiveSequenceValue:
		// Each held instruction counts once. Descending into a held instruction's own value would
		// double count for the same reason the statement case guards against it, so only composite
		// nested values are followed.
		for _, nested := range shape.Instructions {
			result.Instructions++
			if _, plain := nested.Value.(*ReactiveInstructionValue); !plain {
				measureValue(nested.Value, result)
			}
		}
		measureValue(shape.Value, result)
	case *ReactiveLogicalValue:
		measureValue(shape.Left, result)
		measureValue(shape.Right, result)
	case *ReactiveTernaryValue:
		measureValue(shape.Test, result)
		measureValue(shape.Consequent, result)
		measureValue(shape.Alternate, result)
	case *ReactiveOptionalValue:
		measureValue(shape.Value, result)
	}
}

// eachNestedBlock visits every block a terminal contains.
//
// Exhaustive over the thirteen variants deliberately. A `default` arm here would silently skip a
// terminal's body if a variant were added, and the measurement that proves this pass works is a
// count over the whole tree -- so a missed arm would under-report rather than fail loudly.
func eachNestedBlock(terminal ReactiveTerminal, visit func(ReactiveBlock)) {
	switch shape := terminal.(type) {
	case *ReactiveIf:
		visit(shape.Consequent)
		if shape.Alternate != nil {
			visit(*shape.Alternate)
		}
	case *ReactiveSwitch:
		for _, arm := range shape.Cases {
			if arm.Block != nil {
				visit(*arm.Block)
			}
		}
	case *ReactiveWhile:
		visit(shape.Loop)
	case *ReactiveDoWhile:
		visit(shape.Loop)
	case *ReactiveFor:
		visit(shape.Loop)
	case *ReactiveForOf:
		visit(shape.Loop)
	case *ReactiveForIn:
		visit(shape.Loop)
	case *ReactiveLabelTerminal:
		visit(shape.Block)
	case *ReactiveTry:
		visit(shape.Block)
		visit(shape.Handler)
	case *ReactiveBreak, *ReactiveContinue, *ReactiveReturn, *ReactiveThrow:
		// Leaf terminals hold no blocks.
	}
}

// eachTerminalValue visits every value a terminal carries, for measurement.
//
// Only the loop terminals hold values; `if` and `switch` test a bare `Place`, which is not a value
// block and carries no instructions.
// A value that is a bare `ReactiveInstructionValue` IS one instruction, collapsed. `valueOf` returns
// the value directly rather than a one-element sequence, so the count has to attribute it here or
// under-report by one per value block -- which is exactly the residue that remained after the
// sequence descent landed, 10 of 12 rather than 12 of 12 on the `for-of` fixture.
func eachTerminalValue(terminal ReactiveTerminal, visit func(ReactiveValue)) {
	switch shape := terminal.(type) {
	case *ReactiveWhile:
		visit(shape.Test)
	case *ReactiveDoWhile:
		visit(shape.Test)
	case *ReactiveFor:
		visit(shape.Init)
		visit(shape.Test)
		if shape.Update != nil {
			visit(shape.Update)
		}
	case *ReactiveForOf:
		visit(shape.Init)
		visit(shape.Test)
	case *ReactiveForIn:
		visit(shape.Init)
	}
}

// visitBlock emits one block's instructions and then dispatches on its terminal.
//
// The single most important property here is that a block is visited ONCE. `emitted` is the check on
// that and `scheduled` is the mechanism: a block a parent has committed to emitting is not emitted
// by a child, which emits a break to it instead.
func (c *reactiveContext) visitBlock(block *BasicBlock, into *ReactiveBlock) {
	if block == nil {
		return
	}
	if c.emitted[block.Id] {
		// Upstream raises `Cannot emit the same block twice`. Counted and skipped here, because a
		// linter that panics on a graph it dislikes is worse than one that declines a function, and
		// because emitting it twice would produce a tree with duplicated statements -- silently
		// wrong output rather than a visible failure.
		c.doubleEmit++
		return
	}
	c.emitted[block.Id] = true

	for _, instructionId := range block.Instructions {
		instruction := c.function.Instructions[instructionId]
		if instruction == nil {
			continue
		}
		lvalue := instruction.LValue
		*into = append(*into, &ReactiveInstructionStatement{
			Instruction: &ReactiveInstruction{
				Order:  instruction.Order,
				LValue: &lvalue,
				Value:  &ReactiveInstructionValue{Value: instruction.Value},
			},
		})
	}

	c.visitTerminal(block, into)
}

// visitTerminal dispatches one block's terminal, emitting a tree terminal and recursing into arms.
//
// Exhaustive over the HIR's terminal set. The shape of every arm is upstream's: schedule the
// fallthrough so nested blocks emit a break rather than re-emitting it, walk the arms, unschedule,
// then visit the fallthrough as a SIBLING rather than as a child -- which is what flattens
// `if (a) {...} rest` into two statements instead of nesting `rest` inside the if.
func (c *reactiveContext) visitTerminal(block *BasicBlock, into *ReactiveBlock) {
	var scheduleIds []int

	switch terminal := block.Terminal.(type) {
	case *Return:
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveReturn{Value: terminal.Value, Order: terminal.Order},
		})

	case *Throw:
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveThrow{Value: terminal.Value, Order: terminal.Order},
		})

	case *Unreachable, *Unsupported:
		// Neither has a tree form. `unreachable` marks a block control never leaves, and upstream
		// emits nothing for it; `Unsupported` is this tree's own marker for a construct lowering
		// declined, and emitting nothing keeps it out of the output rather than inventing a
		// statement for it.

	case *Goto:
		c.emitGoto(terminal, into)

	case *If:
		fallthroughId, scheduled := c.scheduleFallthrough(terminal.Fallthrough, controlFlowIf, &scheduleIds)
		consequent := c.traverse(terminal.Consequent)
		var alternate *ReactiveBlock
		if terminal.Alternate != terminal.Fallthrough {
			built := c.traverse(terminal.Alternate)
			alternate = &built
		}
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveIf{
				Test:       terminal.Test,
				Consequent: consequent,
				Alternate:  alternate,
				Order:      terminal.Order,
			},
			Label: c.labelFor(terminal.Fallthrough, scheduled),
		})
		c.visitFallthrough(fallthroughId, into)

	case *Branch:
		// `Branch` is the value-block form of `if`, used inside ternaries and logicals. It has no
		// fallthrough of its own -- the enclosing value terminal owns it -- so both arms are walked
		// and nothing is scheduled here.
		consequent := c.traverse(terminal.Consequent)
		built := c.traverse(terminal.Alternate)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveIf{
				Test:       terminal.Test,
				Consequent: consequent,
				Alternate:  &built,
				Order:      terminal.Order,
			},
		})

	case *Switch:
		fallthroughId, scheduled := c.scheduleFallthrough(terminal.Fallthrough, controlFlowSwitch, &scheduleIds)
		// # Cases are walked in REVERSE, and each is scheduled AFTER it is traversed
		//
		// Both halves are upstream's and both are load-bearing. A `switch` with fallthrough has
		// several case labels sharing one block, and the LAST label owning it is the one whose body
		// should carry the statements -- so the walk runs backwards and an earlier label finds the
		// block already scheduled and contributes an empty arm.
		//
		// The first spelling here iterated forwards and scheduled BEFORE traversing, which made a
		// shared block reachable from two arms and emitted it twice. Measured: 8 functions on the
		// corpus double-emitted, and every one of them contained a `Switch` -- which is what
		// identified the arm rather than a fixture, since the attribution was 100%%.
		//
		// A case already scheduled that is NOT the fallthrough is upstream's invariant; here it is
		// simply an empty arm, because a linter must not raise and an empty arm is the honest
		// reading of "this label falls through to the next one".
		reversed := make([]ReactiveSwitchCase, 0, len(terminal.Cases))
		for index := len(terminal.Cases) - 1; index >= 0; index-- {
			arm := terminal.Cases[index]
			testPlace := arm.Test
			var body *ReactiveBlock
			if !c.isScheduled(arm.Block) {
				built := c.traverse(arm.Block)
				body = &built
				scheduleIds = append(scheduleIds, c.schedule(arm.Block, controlFlowCase))
			}
			reversed = append(reversed, ReactiveSwitchCase{Test: testPlace, Block: body})
		}
		cases := make([]ReactiveSwitchCase, len(reversed))
		for index, arm := range reversed {
			cases[len(reversed)-1-index] = arm
		}
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveSwitch{Test: terminal.Test, Cases: cases, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, scheduled),
		})
		c.visitFallthrough(fallthroughId, into)

	case *While:
		fallthroughId := c.scheduleLoopTargets(terminal.Fallthrough, terminal.Test, &scheduleIds)
		test := c.valueOf(terminal.Test)
		loop := c.traverse(terminal.Loop)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveWhile{Test: test, Loop: loop, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, true),
		})
		c.visitFallthrough(fallthroughId, into)

	case *DoWhile:
		fallthroughId := c.scheduleLoopTargets(terminal.Fallthrough, terminal.Test, &scheduleIds)
		loop := c.traverse(terminal.Loop)
		test := c.valueOf(terminal.Test)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveDoWhile{Loop: loop, Test: test, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, true),
		})
		c.visitFallthrough(fallthroughId, into)

	case *For:
		fallthroughId := c.scheduleLoopTargets(terminal.Fallthrough, terminal.Update, &scheduleIds)
		init := c.valueOf(terminal.Init)
		test := c.valueOf(terminal.Test)
		var update ReactiveValue
		if terminal.Update != terminal.Fallthrough {
			update = c.valueOf(terminal.Update)
		}
		loop := c.traverse(terminal.Loop)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveFor{
				Init: init, Test: test, Update: update, Loop: loop, Order: terminal.Order,
			},
			Label: c.labelFor(terminal.Fallthrough, true),
		})
		c.visitFallthrough(fallthroughId, into)

	case *ForOf:
		fallthroughId := c.scheduleLoopTargets(terminal.Fallthrough, terminal.Test, &scheduleIds)
		init := c.valueOf(terminal.Init)
		test := c.valueOf(terminal.Test)
		loop := c.traverse(terminal.Loop)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveForOf{Init: init, Test: test, Loop: loop, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, true),
		})
		c.visitFallthrough(fallthroughId, into)

	case *ForIn:
		fallthroughId := c.scheduleLoopTargets(terminal.Fallthrough, terminal.Loop, &scheduleIds)
		init := c.valueOf(terminal.Init)
		loop := c.traverse(terminal.Loop)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveForIn{Init: init, Loop: loop, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, true),
		})
		c.visitFallthrough(fallthroughId, into)

	case *Label:
		fallthroughId, scheduled := c.scheduleFallthrough(terminal.Fallthrough, controlFlowIf, &scheduleIds)
		body := c.traverse(terminal.Block)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveLabelTerminal{Block: body, Order: terminal.Order},
			Label:    c.labelFor(terminal.Fallthrough, scheduled),
		})
		c.visitFallthrough(fallthroughId, into)

	case *Try:
		fallthroughId, scheduled := c.scheduleFallthrough(terminal.Fallthrough, controlFlowIf, &scheduleIds)
		c.catchHandlers[terminal.Handler] = true
		body := c.traverse(terminal.Block)
		handler := c.traverse(terminal.Handler)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveTry{
				Block: body, HandlerBinding: terminal.HandlerBinding, Handler: handler,
				Order: terminal.Order,
			},
			Label: c.labelFor(terminal.Fallthrough, scheduled),
		})
		c.visitFallthrough(fallthroughId, into)

	case *Scope:
		fallthroughId, scheduled := c.scheduleFallthrough(terminal.Fallthrough, controlFlowIf, &scheduleIds)
		if scheduled {
			// Recorded only when this terminal actually claimed the block. If an enclosing construct
			// already owns it, the break belongs to that construct and eliding it here would drop a
			// jump the source really makes.
			c.scopeFallthroughs[terminal.Fallthrough] = true
		}
		body := c.traverse(terminal.Block)
		c.unscheduleAll(scheduleIds)
		*into = append(*into, &ReactiveScopeBlock{
			Scope:        terminal.Scope,
			Instructions: body,
		})
		c.visitFallthrough(fallthroughId, into)

	case *Logical:
		c.emitValueTerminal(terminal.Fallthrough, terminal.Order, into, &scheduleIds)

	case *Ternary:
		c.emitValueTerminal(terminal.Fallthrough, terminal.Order, into, &scheduleIds)

	case *Optional:
		// Never constructed in this tree; see ReactiveFunctionGapUnbuiltTerminals. Handled because
		// it shares upstream's arm with ternary and logical, so leaving it out would be a silent
		// hole the day the lowering starts emitting one.
		c.emitValueTerminal(terminal.Fallthrough, terminal.Order, into, &scheduleIds)

	case *Sequence:
		// Never constructed in this tree; see ReactiveFunctionGapUnbuiltTerminals.
		c.emitValueTerminal(terminal.Fallthrough, terminal.Order, into, &scheduleIds)

	case *MaybeThrow:
		// Upstream's comment: "ReactiveFunction does not explicitly model maybe-throw semantics, so
		// these terminals flatten away." Never constructed here either, so this arm costs nothing
		// today and is written to match upstream rather than to run.
		if !c.isScheduled(terminal.Continuation) {
			c.visitBlock(c.block(terminal.Continuation), into)
		}
	}
}

// scheduleFallthrough claims a terminal's fallthrough if nothing else has, so nested blocks break
// to it rather than re-emitting it.
//
// Returns the block to visit as a sibling afterwards, and whether this call is the one that claimed
// it. A fallthrough already scheduled by an enclosing construct is that construct's to emit.
func (c *reactiveContext) scheduleFallthrough(block BlockId, kind controlFlowKind,
	ids *[]int) (BlockId, bool) {
	if c.isScheduled(block) {
		return 0, false
	}
	*ids = append(*ids, c.schedule(block, kind))
	return block, true
}

// scheduleLoopTargets claims a loop's fallthrough and its continue block together.
func (c *reactiveContext) scheduleLoopTargets(fallthrough_, continueBlock BlockId,
	ids *[]int) BlockId {
	owns := !c.isScheduled(fallthrough_)
	*ids = append(*ids, c.scheduleLoop(fallthrough_, continueBlock))
	if !owns {
		return 0
	}
	return fallthrough_
}

// visitFallthrough emits the block control resumes at, as a SIBLING of the construct.
//
// This is what keeps the tree flat where the source was flat: statements after an `if` are siblings
// of it, not children. A zero id means the fallthrough belongs to an enclosing construct.
func (c *reactiveContext) visitFallthrough(block BlockId, into *ReactiveBlock) {
	if block == 0 {
		return
	}
	c.visitBlock(c.block(block), into)
}

// labelFor produces the break-target label upstream emits for every terminal with a fallthrough.
//
// Upstream emits these naively and prunes them later; see `ReactiveFunctionGapUnprunedLabels`. The
// `Implicit` flag is upstream's and records that control would reach the target anyway.
func (c *reactiveContext) labelFor(block BlockId, owned bool) *ReactiveLabel {
	if !owned {
		return nil
	}
	return &ReactiveLabel{Id: block, Implicit: true}
}

// emitGoto turns a jump into a break, a continue, or nothing at all.
//
// The three-way answer is the reason `breakTarget` walks a stack. A goto to the innermost target is
// where control transfers implicitly, so upstream emits a terminal marked implicit rather than
// omitting it -- keeping the statement lets a later pass decide, and dropping it here would lose the
// information irrecoverably.
func (c *reactiveContext) emitGoto(terminal *Goto, into *ReactiveBlock) {
	if terminal.Variant == GotoVariantContinue {
		kind, matched := c.continueTarget(terminal.Block)
		if !matched {
			c.unmatchedGotos++
		}
		*into = append(*into, &ReactiveTerminalStatement{
			Terminal: &ReactiveContinue{
				Target: terminal.Block, TargetKind: kind, Order: terminal.Order,
			},
		})
		return
	}
	if !c.isScheduled(terminal.Block) {
		// Not a break at all: the target has not been claimed by any enclosing construct, so control
		// simply continues into it and it is emitted inline.
		c.visitBlock(c.block(terminal.Block), into)
		return
	}
	kind, matched := c.breakTarget(terminal.Block)
	if !matched {
		c.unmatchedGotos++
	}
	if c.scopeFallthroughs[terminal.Block] {
		// Upstream's `visitBreak` returns nothing here, and asserts the target is implicit while
		// doing so. A reactive scope falls through to its continuation with no `break` in the
		// source, so emitting one invents a jump and names a target no enclosing construct claims.
		//
		// The assert is kept as a measurement rather than a panic, which is this package's standing
		// treatment of upstream invariants: a linter reports where a compiler aborts.
		if kind != ReactiveTargetImplicit {
			c.nonImplicitScopeBreaks++
		}
		c.elidedScopeBreaks++
		return
	}
	*into = append(*into, &ReactiveTerminalStatement{
		Terminal: &ReactiveBreak{Target: terminal.Block, TargetKind: kind, Order: terminal.Order},
	})
}

// emitValueTerminal handles the four terminals that are EXPRESSIONS in the tree.
//
// `Logical`, `Ternary`, `Optional` and `Sequence` are control flow in the graph and values here.
// Upstream reconstructs the composite value from the arms' value blocks; this emits the fallthrough
// and records the gap, because reconstructing the expression needs the value-block extraction that
// `extractValueBlockResult` performs and that extraction depends on phi elimination this tree
// performs differently. See `ReactiveFunctionGapValueExpressions`.
func (c *reactiveContext) emitValueTerminal(fallthrough_ BlockId, order EvaluationOrder,
	into *ReactiveBlock, ids *[]int) {
	blockId, _ := c.scheduleFallthrough(fallthrough_, controlFlowIf, ids)
	c.unscheduleAll(*ids)
	c.visitFallthrough(blockId, into)
}

// traverse builds a fresh block from one entry point.
func (c *reactiveContext) traverse(block BlockId) ReactiveBlock {
	var built ReactiveBlock
	c.visitBlock(c.block(block), &built)
	return built
}

// valueOf reads a value block down to the expression it computes, KEEPING its instructions.
//
// Upstream's `visitValueBlock` plus `valueBlockResultToSequence`. A value block -- a loop's `test`,
// a `for`'s `init`, the arms of a ternary -- is a block whose purpose is to compute one value, and
// in the tree it becomes an EXPRESSION rather than a sequence of statements.
//
// # The defect this replaced, and how it was found
//
// The first spelling returned only the block's LAST instruction value. That is the right answer for
// a block holding a single computation, and it silently discarded everything else: measured over the
// corpus, 380 of 677 functions came out with fewer instructions than they went in with. Attribution
// separated the two causes -- 353 of those had a value TERMINAL, which is the declared
// `ReactiveFunctionGapValueExpressions`, but 27 did not, and every one of them held a `ForOf` whose
// `Test` and `Init` are value blocks read through here.
//
// So the loop arms were losing real instructions while the cross-tabulation still looked healthy,
// which is exactly the failure a count of statements cannot see and an input-versus-output
// comparison can. The sequence wrapper is upstream's answer and it is reproduced here: every
// instruction before the result is kept, nested inside the value.
//
// The block is marked emitted so the ordinary walk does not visit it again as a statement block --
// a value block belongs to its terminal, not to the enclosing statement list.
func (c *reactiveContext) valueOf(block BlockId) ReactiveValue {
	basic := c.block(block)
	if basic == nil || len(basic.Instructions) == 0 {
		return nil
	}
	if c.emitted[basic.Id] {
		c.doubleEmit++
		return nil
	}
	c.emitted[basic.Id] = true

	instructions := make([]*ReactiveInstruction, 0, len(basic.Instructions))
	for _, instructionId := range basic.Instructions {
		instruction := c.function.Instructions[instructionId]
		if instruction == nil {
			continue
		}
		lvalue := instruction.LValue
		instructions = append(instructions, &ReactiveInstruction{
			Order:  instruction.Order,
			LValue: &lvalue,
			Value:  &ReactiveInstructionValue{Value: instruction.Value},
		})
	}
	if len(instructions) == 0 {
		return nil
	}

	// The last instruction IS the value; everything before it runs for effect. A block with a single
	// instruction needs no sequence wrapper, which keeps the ordinary case flat.
	result := instructions[len(instructions)-1]
	if len(instructions) == 1 {
		return result.Value
	}
	return &ReactiveSequenceValue{
		Instructions: instructions[:len(instructions)-1],
		Order:        result.Order,
		Value:        result.Value,
	}
}
