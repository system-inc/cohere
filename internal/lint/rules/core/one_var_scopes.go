package core

// oneVarScopes carries the two stacks upstream maintains while walking.
type oneVarScopes struct {
	functions []oneVarStateFlags
	blocks    []oneVarBlockFrame
}

func (scopes *oneVarScopes) startBlock() {
	scopes.blocks = append(scopes.blocks, oneVarBlockFrame{})
}

func (scopes *oneVarScopes) endBlock() {
	if len(scopes.blocks) > 0 {
		scopes.blocks = scopes.blocks[:len(scopes.blocks)-1]
	}
}

func (scopes *oneVarScopes) startFunction() {
	scopes.functions = append(scopes.functions, oneVarStateFlags{})
	scopes.startBlock()
}

func (scopes *oneVarScopes) endFunction() {
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
func (scopes *oneVarScopes) current(kind oneVarKind) *oneVarStateFlags {
	if kind == oneVarKindVar {
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
	case oneVarKindLet:
		return &frame.Let
	case oneVarKindConst:
		return &frame.Const
	case oneVarKindUsing:
		return &frame.Using
	case oneVarKindAwaitUsing:
		return &frame.AwaitUsing
	}
	return nil
}
