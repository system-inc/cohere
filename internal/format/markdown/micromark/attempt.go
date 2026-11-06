//cohere:file-ignore consistency-require-separated-folder-words the micromark library's name
package micromark

import "slices"

// attempt is one call of an attempt, check or interrupt. Upstream keeps its state in a closure's
// variables; here it is one struct, and its states are bound once per struct (newAttempt), so trying a
// construct allocates no closure of its own (#93dpede). A Memory reuses the struct, bound states and
// all, in later parses (#vbjv3d6).
type attempt struct {
	context                 *TokenizeContext
	onReturn                func(*Construct, *storeInfo)
	interrupt               bool
	returnState, bogusState State

	listOfConstructs []*Construct
	constructIndex   int
	currentConstruct *Construct
	info             storeInfo

	// pending is the construct start begins: the one handleConstruct named, which start reads before any
	// other construct of this attempt can be named.
	pending *Construct
	// single holds a lone construct as a list, without allocating one.
	single [1]*Construct
	// record is the record startRecord reads, the one handleMapOfConstructs was given.
	record *ConstructRecord
	// self is what the current construct tokenizes with. A construct's states are done with it once
	// they return ok or nok, before the next construct of this attempt starts and sets it again.
	self Self

	ok, nok, start, startMap State
}

// release zeroes the attempt for its next parse, keeping its bound states.
func (run *attempt) release() {
	*run = attempt{ok: run.ok, nok: run.nok, start: run.start, startMap: run.startMap}
}

func (run *attempt) handleListOfConstructs(list []*Construct) State {
	run.listOfConstructs = list
	run.constructIndex = 0

	if len(list) == 0 {
		if run.bogusState == nil {
			// Invariant: the only attempts in this package that pass no bogus state name a single construct,
			// which becomes a one-item list, so an empty list always comes with a bogus state.
			panic("micromark: expected `bogusState` to be given")
		}
		return run.bogusState
	}

	return run.handleConstruct(list[run.constructIndex])
}

func (run *attempt) handleMapOfConstructs(record *ConstructRecord) State {
	run.record = record
	return run.startMap
}

// startRecord is handleMapOfConstructs' start: the constructs for the code, then those for every code.
func (run *attempt) startRecord(code Code) State {
	return run.handleListOfConstructs(run.record.constructsAt(code))(code)
}

func (run *attempt) handleConstruct(construct *Construct) State {
	run.pending = construct
	return run.start
}

func (run *attempt) startPending(code Code) State {
	construct, context := run.pending, run.context
	run.info = context.store()
	run.currentConstruct = construct

	if !construct.Partial {
		context.CurrentConstruct = construct
	}

	if construct.Name != "" && slices.Contains(context.Parser.Constructs.Disable, construct.Name) {
		return run.nok(code)
	}

	run.self = Self{TokenizeContext: context, isView: run.interrupt, viewInterrupt: run.interrupt}
	return construct.Tokenize(&run.self, context.effects, run.ok, run.nok)(code)
}

func (run *attempt) succeed(code Code) State {
	if code != run.context.expectedCode {
		// Invariant: a construct returns ok or nok with the code its state was handed, as upstream's
		// development build asserts, so a mismatch is a porting bug, never something the input can cause.
		panic("micromark: expected code")
	}
	run.context.consumed = true
	run.onReturn(run.currentConstruct, &run.info)
	return run.returnState
}

func (run *attempt) fail(code Code) State {
	if code != run.context.expectedCode {
		// Invariant: a construct returns ok or nok with the code its state was handed, as upstream's
		// development build asserts, so a mismatch is a porting bug, never something the input can cause.
		panic("micromark: expected code")
	}
	run.context.consumed = true
	run.info.restore()

	run.constructIndex++
	if run.constructIndex < len(run.listOfConstructs) {
		return run.handleConstruct(run.listOfConstructs[run.constructIndex])
	}

	return run.bogusState
}
