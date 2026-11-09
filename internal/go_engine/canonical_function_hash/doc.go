// Package canonical_function_hash hashes each Go function canonically, for `cohere --duplicates` (#jfvhbj7): two
// functions get one digest exactly when they compute the same thing, whatever their names or the order of their
// independent statements.
//
// It reads the engine's own intermediate representation (internal/go_engine/intermediate_representation), the very
// functions rules/build_intermediate_representation builds once per package for the rules on it, in single-assignment
// form with locals lifted, and hashes them by the spec proven on #6rj7jw7 (its attachment duplicate-hash-spec.md):
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
// The representation is built with no-return calls marked, where the prototype built without; its fixture tables are
// run on it here (canonical_function_hash_test.go). A debug reference never reaches the hash, since the builder strips
// each into the function's expression map. A function holding an instruction this hash does not model is refused by
// name, never guessed past. It read staticcheck's go/ir through buildir until #kk4864m, and reports the same groups of
// duplicate functions on our representation over cohere's own tree and the deeafbf4 corpus.
package canonical_function_hash
