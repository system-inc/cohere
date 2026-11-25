// Package holder declares a package-level uintptr named Data, so parity can select it through a package name, a
// selector whose left side has no type.
package holder

// Data is a uintptr that holds no pointer the garbage collector can see.
var Data uintptr
