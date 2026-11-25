package parity

// This file imports fmt already, so the first fix names it and adds no import, except where a local name shadows fmt,
// and the import is added again under the first name nothing holds.

import "fmt"

func imported(number int) {
	_ = fmt.Sprint(number)
	_ = string(number)
	_ = Text(number)
}

func shadowed(number int) {
	fmt := number
	_ = string(fmt)
	{
		fmt0 := fmt
		_ = Text(fmt0)
	}
}
