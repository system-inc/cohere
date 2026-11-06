package flagged

// A variant with a real method beside its marker is a type with behavior. Return only marks itself a Statement and
// stays quiet beside Block, while Assign answers its name here and Loop repeats a statement in loop_body.go, so each
// is a second type with methods.
type Statement interface {
	statementValue()
}

type Block struct {
	statements []Statement
}

func (block *Block) Length() int {
	return len(block.statements)
}

type Return struct{}

func (Return) statementValue() {}

type Assign struct { // want "already serves `Block`, and `Assign` is a second type with methods"
	name string
}

func (Assign) statementValue() {}

func (assign Assign) Name() string {
	return assign.name
}

type Loop struct { // want "already serves `Block`, and `Loop` is a second type with methods"
	body Block
}

func (Loop) statementValue() {}
