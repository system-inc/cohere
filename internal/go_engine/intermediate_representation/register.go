package intermediate_representation

import (
	"fmt"
	"go/types"
)

// register is a mix-in embedded by all IR values that are also
// instructions, i.e. virtual registers, and provides a uniform
// implementation of most of the Value interface: Value.Name() is a
// numbered register (e.g. "t0"); the other methods are field accessors.
//
// Temporary names are automatically assigned to each register on
// completion of building a function in IR form.
type register struct {
	anInstruction
	typ       types.Type // type of virtual register
	referrers []Instruction
}

func (register *register) Type() types.Type { return register.typ }

func (register *register) setType(typ types.Type) { register.typ = typ }

func (register *register) Name() string { return fmt.Sprintf("t%d", register.id) }

func (register *register) Referrers() *[]Instruction { return &register.referrers }
