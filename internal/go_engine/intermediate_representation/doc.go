// Package intermediate_representation is the Go engine's own IR: a function's blocks and instructions,
// lowered from its syntax and types, for the checks that read control and data flow (#dz6qbwm).
//
// It is staticcheck's go/ir (honnef.co/go/tools v0.8.1), copied and renamed to the house's words by
// the table on #7zbpd7w, so its output matches what buildir gives readers today. Ours differs from
// go/ir in what it leaves out:
//   - Lifting's own algorithm. Ours lifts through the shared core's construction, in lifting.go, where
//     go/ir places phis by dominance frontiers, and the values reaching each use match go/ir's (#ee33njz).
//   - Debug references and the expression map, which wait for a reader that asks (#ee33njz).
//   - Constant composite simplification, which go/ir has switched off and buildir never runs.
//   - Monomorphized generics. A generic body is built once, in generic form, and a call to an
//     instance goes through an instantiation wrapper, as go/ir does without InstantiateGenerics.
//   - The builder modes, sanity checks and printer, but for Constant.String, whose value:type form
//     readers' messages repeat.
//   - Program.RuntimeTypes, MethodValue and LookupMethod, which no reader reads.
package intermediate_representation
