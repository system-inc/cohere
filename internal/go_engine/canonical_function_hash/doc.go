// Package canonical_function_hash hashes each Go function canonically, for `cohere --duplicates` (#jfvhbj7): two
// functions get one digest exactly when they compute the same thing, whatever their names or the order of their
// independent statements.
//
// It reads staticcheck's intermediate representation, the very functions buildir builds once per package for
// staticcheck's checks and the engine's IR dispatcher (rules/staticcheck_ir), in single-assignment form with locals
// lifted, and hashes them by the spec proven on #6rj7jw7 (its attachment duplicate-hash-spec.md):
//
//   - Values by structure: a Merkle hash of kind, type, payload and operand digests. Parameters and free variables by
//     position, constants by value (`16` and `0x10` agree). A value's type is part of it, so an int addition and a
//     float one differ, and types are named by full package path.
//   - Callees by identity: a function by its full name, a closure made here by its own body.
//   - Commutative operators sort their operands only on numbers, since string concatenation is not commutative.
//   - Effects keep their order: calls, stores, sends, allocations and returns chain per block, and a load or map
//     lookup hashes with the chain before it, so it floats among reads but never across a write.
//   - Blocks by a depth-first preorder from the entry, true successor first; phis named by partition refinement over
//     their incoming edges, so loop-carried values that differ get different names without a cycle in the hash.
//
// buildir builds in ir.GlobalDebug mode with no-return calls marked, where the prototype built with neither; its
// fixture tables are run on buildir's IR here (canonical_function_hash_test.go), and a probe of every instruction
// kind buildir emitted for them found no debug reference to skip. A function holding an instruction this hash does
// not model is refused by name, never guessed past.
package canonical_function_hash
