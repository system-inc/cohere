package lookalike

// Engine is the one type this file serves. Everything else here uses Gear, which has methods in gear.go, without
// declaring a method of it.
type Engine struct {
	gears []*Gear
}

func (engine *Engine) Run() {
	for _, gear := range engine.gears {
		gear.Turn()
	}
}

// A function with a parameter of another type with methods is no method.
func Start(engine *Engine, gear *Gear) {
	engine.gears = append(engine.gears, gear)
}

// A function returning one, and a method expression and a method value of it, declare nothing.
func NewGear() *Gear {
	return &Gear{}
}

var turn = (*Gear).Turn

var turnSpare = NewGear().Turn

// An alias declares no type of its own, and a type defined from Gear inherits none of its methods.
type Shortcut = Gear

type Spare Gear

// A struct embedding a type with methods is promoted those methods but declares none.
type Car struct {
	*Gear
}
