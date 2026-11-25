package main

// Engine is the one type this file serves, and the file is named for it. Everything else here uses Gear, which has
// methods in gear.go, without declaring a method of it, so Gear is never the type this file is judged against.
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
