package check

import (
	"go/types"
	"testing"
)

// fieldsHolderPath is the path of the package each field fixture's Box is declared in, whose unexported field inner
// is of the type hidden, declared once per target.
const fieldsHolderPath = "example.com/fields/holder"

// fieldsFormatSource is a stand-in for fmt, at fmt's path, whose State printf's walk reads by its path and name.
const fieldsFormatSource = "package fmt\n\ntype State interface{ Write(b []byte) (int, error) }\n"

// formatted gives hidden a Format method, so it answers every printf verb alike, and a fixture's two targets differ
// only in another property.
const formatted = "\n\nfunc (hidden) Format(fmt.State, rune) {}\n"

// fieldsFixture is a module whose package holder declares Box, a struct with one unexported field, inner, of the
// type hidden, declared as darwin and as linux, each followed by extra, which gives it methods on both targets.
func fieldsFixture(darwin string, linux string, extra string) targetFixture {
	// Each file names fmt.State, so its import is used whether or not hidden has a Format method.
	header := "package holder\n\nimport \"fmt\"\n\nvar _ fmt.State\n\n"
	return targetFixture{
		files: map[string]string{
			"fmt/print.go":            fieldsFormatSource,
			"holder/holder.go":        header + "type Box struct{ inner hidden }\n",
			"holder/hidden_darwin.go": header + "type hidden " + darwin + extra + "\n",
			"holder/hidden_linux.go":  header + "type hidden " + linux + extra + "\n",
		},
		packages: []targetPackage{{"fmt", "fmt"}, {"holder", fieldsHolderPath}},
	}
}

// boxReader is a user package's source that reads holder.Box.
const boxReader = "func Use(box holder.Box) holder.Box { return box }\n"

// amd64 are the sizes every field fixture's builds are told.
var amd64 = types.SizesFor("gc", "amd64")

// An importer reading a struct of another package whose unexported field's type differs by target, as os.File's
// *file does, is restored when no property a rule reads of the field differs: the field's type is rendered by those
// properties, never by its declaration, which no importer can reach.
func TestAnUnexportedFieldWhosePropertiesHoldIsRestored(test *testing.T) {
	test.Parallel()

	fixture := fieldsFixture("struct{ count uint64 }", "struct{ total int64 }", "")
	if holds, why := readsHoldAcross(test, fixture, fieldsHolderPath, boxReader, amd64, false); !holds {
		test.Errorf("want the reads of Box to hold, inner's properties alike on both targets, got %q", why)
	}
}

// An importer reading a struct of another package whose unexported field differs by target in one property a rule
// reads is checked again, for each of the properties: two targets whose field differs in that one alone.
func TestAnUnexportedFieldWhosePropertyDiffersIsCheckedAgain(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		property string
		darwin   string
		linux    string
		extra    string
	}{
		{"size and alignment", "struct{ count uint32 }", "struct{ count uint64 }", ""},
		{"comparable", "struct{ handlers [0]func() }", "struct{ handlers [0]chan int }", formatted},
		{"lock", "struct{ count int64 }", "struct{ count int64 }", ""},
		{"printf", "struct{ code int32 }", "struct{ code uint32 }", ""},
		{"binary", "struct{ count int }", "struct{ count int64 }", ""},
		{"float", "struct{ reading float64 }", "struct{ reading uint64 }", formatted},
		{"basic", "struct{ address *int }", "struct{ address uintptr }", formatted},
	} {
		test.Run(step.property, func(test *testing.T) {
			test.Parallel()

			fixture := fieldsFixture(step.darwin, step.linux, step.extra)
			if step.property == "lock" {
				// The darwin hidden is a lock, its pointer a sync.Locker, which the linux one is not.
				fixture.files["holder/hidden_darwin.go"] += "\nfunc (*hidden) Lock() {}\n\nfunc (*hidden) Unlock() {}\n"
			}
			want := "T object " + fieldsHolderPath + ".Box renders differently"
			holds, why := readsHoldAcross(test, fixture, fieldsHolderPath, boxReader, amd64, false)
			if holds || why != want {
				test.Errorf("want %q, inner's %s differing by target, got %t, %q", want, step.property, holds, why)
			}
		})
	}
}

// A package with assembly files, whose frames asmdecl lays out field by field, is judged by every type it reaches
// rendered whole: it is checked again when another package's unexported field it reaches differs by target, though
// no property does, and restored when nothing it reaches differs.
func TestAPackageWithAssemblyIsJudgedByEveryFieldWhole(test *testing.T) {
	test.Parallel()

	differing := fieldsFixture("struct{ count uint64 }", "struct{ total int64 }", "")
	want := "T object " + fieldsHolderPath + ".Box renders differently"
	if holds, why := readsHoldAcross(test, differing, fieldsHolderPath, boxReader, amd64, true); holds || why != want {
		test.Errorf("want %q, inner's type differing by target, got %t, %q", want, holds, why)
	}
	alike := fieldsFixture("struct{ count uint64 }", "struct{ count uint64 }", "")
	if holds, why := readsHoldAcross(test, alike, fieldsHolderPath, boxReader, amd64, true); !holds {
		test.Errorf("want the reads of Box to hold, nothing differing by target, got %q", why)
	}
}
