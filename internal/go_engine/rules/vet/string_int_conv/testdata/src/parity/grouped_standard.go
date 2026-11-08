package parity

// This file groups standard packages, so the first fix adds fmt's spec before the first of them.

import (
	"os"
	"strings"
)

func groupedStandard(number int) {
	_ = os.Args
	_ = strings.ToUpper(string(number))
	_ = Label(number)
}
