package fork

// N is 2 on windows.
const N = 2

// Kill names its second parameter sig on windows.
var Kill func(pid int, sig int) error

// Stat's Mode is a uint16 on windows.
type Stat struct{ Mode uint16 }
