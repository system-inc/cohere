package flagged

import "syscall"

// A file importing syscall is not a pass: a constant with no tie to a system constant is ours.
const LOCAL_FLAG = 1 // want "`LOCAL_FLAG` is in screaming snake case; spell it `LocalFlag`"

// Only a constant mirrors a system constant; a variable holding one is ours to name.
var O_RDONLY_COPY = syscall.O_RDONLY // want "`O_RDONLY_COPY` is in screaming snake case; spell it `ORdonlyCopy`"

// Compared against another of our constants, not a system one, so nothing ties it to syscall.
const OTHER_FLAG = 2 // want "`OTHER_FLAG` is in screaming snake case; spell it `OtherFlag`"

var flagsMatch = LOCAL_FLAG == OTHER_FLAG

// A comparison against a system constant through arithmetic is not a direct mirror.
const SHIFTED_FLAG = 4 // want "`SHIFTED_FLAG` is in screaming snake case; spell it `ShiftedFlag`"

var shiftedMatch = SHIFTED_FLAG+1 == syscall.O_RDONLY

// Names in another script fold the same way, by rune.
var größe_wert = 1 // want "`größe_wert` has an underscore in it; spell it `größeWert`"

var Ärger_zahl = 1 // want "`Ärger_zahl` has an underscore in it; spell it `ÄrgerZahl`"
