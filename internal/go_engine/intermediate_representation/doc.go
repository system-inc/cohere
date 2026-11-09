// Package intermediate_representation is the Go engine's own IR: a function's blocks and instructions,
// lowered from its syntax and types, for the checks that read control and data flow (#dz6qbwm).
//
// It is staticcheck's go/ir (honnef.co/go/tools v0.8.1), copied and renamed to the house's words by
// the table on #7zbpd7w, so its output matches what buildir gives readers today. Ours differs from
// go/ir in how it lifts and in what it leaves out:
//   - Lifting runs through the shared core's construction (lifting.go) where go/ir places phis by
//     dominance frontiers; the values reaching each use match go/ir's (#ee33njz).
//   - The expression map, ValueForExpression, is recorded per package, only where SetDebugMode asks,
//     where go/ir's GlobalDebug records it for every package.
//   - A package can carry its own no-return predicate (Package.SetNoReturn), and be created while
//     others build, so one program can hold a run's packages each built as its own program would
//     build it, which the fact resolver's does (#w7zmvat).
//   - A package created from its types alone creates each member, a function, variable, constant or
//     type, on its first lookup, where go/ir creates every object of its scope in CreatePackage: an
//     importer's build touches few of them (#npb86tz). Package.Members is a method so that a walk of
//     them all creates them first.
//   - Constant composite simplification, which go/ir has switched off and buildir never runs.
//   - Monomorphized generics. A generic body is built once, in generic form, and a call to an
//     instance goes through an instantiation wrapper, as go/ir does without InstantiateGenerics.
//   - The builder modes, sanity checks and printer, but for Constant.String, whose value:type form
//     readers' messages repeat.
//   - Program.RuntimeTypes, MethodValue and LookupMethod, which no reader reads.
package intermediate_representation
