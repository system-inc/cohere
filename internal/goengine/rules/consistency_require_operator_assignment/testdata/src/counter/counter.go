// Package counter holds a variable another package assigns through its qualified name.
package counter

// Total is assigned as counter.Total by the fixtures.
var Total int

// Limit is a constant, which is no place to assign, read by the fixtures as a key.
const Limit = 3
