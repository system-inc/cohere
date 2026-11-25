package closures

// Inner holds Length bytes, 4 everywhere but linux and 8 on linux, so its underlying moves while its name, a struct
// spelled in place, reads alike: its underlying bit is set, and its name bit is not.
type Inner struct {
	cells [Length]byte
}

// Holder points at an Inner, so block 1 reads Inner through it by name alone and sets neither of Holder's bits, while
// Holder's closure digest, which renders every type its closure reaches, moves with Inner's: only Inner's underlying
// bit covers it. The field is exported, since an unexported one renders by its properties alone, a pointer's size,
// which no build moves.
type Holder struct {
	Inner *Inner
}
