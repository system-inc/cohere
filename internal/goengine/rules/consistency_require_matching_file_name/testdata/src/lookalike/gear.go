package main

type Gear struct {
	teeth int
}

func (gear *Gear) Turn() {
	gear.teeth++
}
