package adamic

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestNoDefiniteAssignmentFiresOnBothForms: probe h09, and the variable form.
func TestNoDefiniteAssignmentFiresOnBothForms(test *testing.T) {
	test.Parallel()
	for name, fixture := range map[string]struct {
		source string
		span   string
	}{
		// h09
		"a field":         {`class Account { balance!: number; describe(): string { return this.balance.toFixed(2); } }`, "balance"},
		"a variable":      {`let total!: number; function read(): number { return total; }`, "total"},
		"a private field": {`class Account { #balance!: number; read(): number { return this.#balance; } }`, "#balance"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			result := runAdamic(test, NoDefiniteAssignment, fixture.source)
			rule_testing.ExpectFindings(test, result, "definiteAssignment")
			expectSpans(test, fixture.source, result, fixture.span)
		})
	}
}

// TestNoDefiniteAssignmentStaysCleanWhereNothingIsClaimed: an initializer, an optional field, and the
// expression form, which is no-non-null-assertion's.
func TestNoDefiniteAssignmentStaysCleanWhereNothingIsClaimed(test *testing.T) {
	test.Parallel()
	for name, source := range map[string]string{
		"an initializer":      `class Account { balance = 0; }`,
		"an optional field":   `class Account { balance?: number; }`,
		"a constructor":       `class Account { balance: number; constructor() { this.balance = 0; } }`,
		"the expression form": `declare const ages: Map<string, number>; const age = ages.get('Ahra')!;`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runAdamic(test, NoDefiniteAssignment, source))
		})
	}
}

// TestNoDefiniteAssignmentStaysCleanOnAdamicsPrograms: Adamic 0.1 compiles all ten.
func TestNoDefiniteAssignmentStaysCleanOnAdamicsPrograms(test *testing.T) {
	test.Parallel()
	expectProgramsClean(test, NoDefiniteAssignment)
}
