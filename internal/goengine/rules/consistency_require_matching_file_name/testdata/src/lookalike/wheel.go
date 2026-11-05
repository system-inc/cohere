package main

// A method declared through an alias of Wheel is Wheel's, so the file serves Wheel and is named for it.
type Wheel struct {
	spokes int
}

type Rim = Wheel

func (rim *Rim) Spin() {
	rim.spokes++
}
