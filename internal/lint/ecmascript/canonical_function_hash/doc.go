// Package canonical_function_hash hashes each TypeScript function canonically, for `cohere --duplicates` (#jfvhbj7):
// two functions get one digest exactly when they compute the same thing, whatever their names, the order of their
// independent statements, or the syntax that lowers alike.
//
// It lowers each function to the high-level intermediate representation in single-assignment form, against its
// file's checker, and hashes the graph by the spec proven on #6rj7jw7 (its attachment duplicate-hash-spec.md):
//
//   - Values by structure, never by source name or position: a Merkle hash of kind, payload and operand digests.
//     Parameters by position, constants by value (`16` and `0x10` agree), copies transparent.
//   - Callees by identity: an import as its declaring file, relative to the project root, and name, never the
//     specifier as written; a function of the same file by its own body; `this` never as a global.
//   - Commutative operators sort their operands only when both sides are numbers, or both bigints, since on anything
//     else each operand's valueOf runs in source order. `&&` and `||` are control flow, never sorted.
//   - Effects keep their order: calls, writes, throws, returns, awaits and allocations chain, and every load that can
//     observe one (a property, a mutable global, a captured binding) hashes with the chain it reads. Pure values float
//     free, which is what lets independent statements reorder.
//   - Blocks by the hash's own reverse postorder over real edges, each straight-line jump fused into the block before
//     it, so an early return and an else agree, and so do a ternary and an if. Loop phis by header and index.
//
// It sits beside the IR rather than inside it: the hash reads nothing the IR keeps unexported, and the IR stays what
// the React rules read. A function the IR cannot read faithfully is refused by name (duplicate_functions.Refusal),
// never guessed past: the one false match the prototype made came from a lowering gap.
package canonical_function_hash
