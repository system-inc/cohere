package core

// oneVariableScopes carries the two stacks upstream maintains while walking.
type oneVariableScopes struct {
	functions []oneVariableStateFlags
	blocks    []oneVariableBlockFrame
}

func (scopes *oneVariableScopes) startBlock() {
	scopes.blocks = append(scopes.blocks, oneVariableBlockFrame{})
}

func (scopes *oneVariableScopes) endBlock() {
	if len(scopes.blocks) > 0 {
		scopes.blocks = scopes.blocks[:len(scopes.blocks)-1]
	}
}

func (scopes *oneVariableScopes) startFunction() {
	scopes.functions = append(scopes.functions, oneVariableStateFlags{})
	scopes.startBlock()
}

func (scopes *oneVariableScopes) endFunction() {
	if len(scopes.functions) > 0 {
		scopes.functions = scopes.functions[:len(scopes.functions)-1]
	}
	scopes.endBlock()
}

// current returns a pointer to the state a declaration kind reads and writes.
//
// A pointer, because upstream mutates the frame in place through an aliased reference and the
// recorded flags have to outlive the call. Returning a copy makes `recordTypes` write to nothing,
// which reads as a rule that never remembers a previous declaration -- every `always` case would
// then pass and only the corpus could tell. A mutant dropping the `var` arm is caught by 91 lines.
func (scopes *oneVariableScopes) current(kind oneVariableKind) *oneVariableStateFlags {
	if kind == oneVariableKindVariable {
		if len(scopes.functions) == 0 {
			return nil
		}
		return &scopes.functions[len(scopes.functions)-1]
	}
	if len(scopes.blocks) == 0 {
		return nil
	}
	frame := &scopes.blocks[len(scopes.blocks)-1]
	switch kind {
	case oneVariableKindLet:
		return &frame.Let
	case oneVariableKindConst:
		return &frame.Const
	case oneVariableKindUsing:
		return &frame.Using
	case oneVariableKindAwaitUsing:
		return &frame.AwaitUsing
	}
	return nil
}
