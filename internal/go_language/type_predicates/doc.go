// Package type_predicates holds the walks of a type the rules ask of a value and the restore from the host's run asks
// of another package's unexported field, each the one copy both call (#b2t9xmf). An importer can never name another
// package's unexported, non-embedded field, so what it can learn of one is only what a rule's walk of the struct finds
// in the field's type; Observe renders exactly that, in place of the type's whole declaration, so a build whose field
// differs only where no walk looks restores the importer from the host. A rule that walks such a field for anything
// else needs a property here, or the guard test (guard_test.go) fails naming its file.
package type_predicates
