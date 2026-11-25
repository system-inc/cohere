package arrays

// Block is windows's, a slice, whose length is no constant.
type Block []byte

// Counted is windows's, an array of 13, since a constant's length needs an array on every platform.
type Counted [13]byte
