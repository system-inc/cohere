package fork

// N is 1 on linux, 2 on windows and 3 elsewhere, the declaration the census must see differ.
const N = 1

// Kill names its second parameter signum on linux and sig elsewhere, as syscall.Kill does: the types are identical, so
// neither the declaration nor a use of it forks.
var Kill func(pid int, signum int) error

// Stat's Mode is a uint32 on linux and elsewhere and a uint16 on windows, as syscall.Stat_t's is between linux and
// darwin.
type Stat struct{ Mode uint32 }
