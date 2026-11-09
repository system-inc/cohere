// Package intermediate_representation is the Go engine's own IR: a function's blocks and instructions,
// lowered from its syntax and types, for the checks that read control and data flow (#dz6qbwm).
//
// It is staticcheck's go/ir (honnef.co/go/tools v0.8.1), copied and renamed, so its output matches
// what buildir gives readers today. Ours differs from go/ir in what it leaves out:
//   - Lifting. Every function is built as go/ir's NaiveForm builds it, locals kept as Alloc, Load and
//     Store, until lifting lands (#ee33njz). finishBody marks the spot.
//   - Debug references and the expression map, which come with lifting (#ee33njz).
//   - Monomorphized generics. A generic body is built once, in generic form, and a call to an
//     instance goes through an instantiation wrapper, as go/ir does without InstantiateGenerics.
//   - The builder modes, sanity checks and printer, but for Const.String, whose value:type form
//     readers' messages repeat.
//   - Program.RuntimeTypes, MethodValue and LookupMethod, which no reader reads.
package intermediate_representation
