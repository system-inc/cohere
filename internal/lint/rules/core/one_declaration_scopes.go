package core

// oneDeclarationScopes carries the two stacks upstream maintains while walking.
type oneDeclarationScopes struct {
	functions []oneDeclarationStateFlags
	blocks    []oneDeclarationBlockFrame
}

func (scopes *oneDeclarationScopes) startBlock() {
	scopes.blocks = append(scopes.blocks, oneDeclarationBlockFrame{})
}

func (scopes *oneDeclarationScopes) endBlock() {
	if len(scopes.blocks) > 0 {
		scopes.blocks = scopes.blocks[:len(scopes.blocks)-1]
	}
}

func (scopes *oneDeclarationScopes) startFunction() {
	scopes.functions = append(scopes.functions, oneDeclarationStateFlags{})
	scopes.startBlock()
}

func (scopes *oneDeclarationScopes) endFunction() {
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
func (scopes *oneDeclarationScopes) current(kind oneDeclarationKind) *oneDeclarationStateFlags {
	if kind == oneDeclarationKindVarKeyword {
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
	case oneDeclarationKindLet:
		return &frame.Let
	case oneDeclarationKindConst:
		return &frame.Const
	case oneDeclarationKindUsing:
		return &frame.Using
	case oneDeclarationKindAwaitUsing:
		return &frame.AwaitUsing
	}
	return nil
}
