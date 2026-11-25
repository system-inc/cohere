//go:build !purego

package flagged

// A variant takes one word after the type's name. Two are a theme and a variant, which a file declaring its type
// does not take, and the name suggested keeps the variant.
type Meter struct { // want "`Meter` is the one type .* Name it `meter_asm.go`.$"
	reading int
}

func (meter *Meter) Read() int {
	return meter.reading
}
