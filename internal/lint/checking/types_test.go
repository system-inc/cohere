package type_checking

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

var unionTypePartsSink *checker.Type

// TestUnionTypePartsSequenceAllocatesNothing holds the reason UnionTypePartsSeq exists: a range loop over
// it allocates nothing for a type that is not a union, where UnionTypeParts allocates its
// one-element slice. The first half proves the measurement can see an allocation, so a zero in the
// second half is the iterator's and not the instrument's.
//
// Not parallel: testing.AllocsPerRun reads the process-wide allocation count, so a test running
// beside it would add its allocations to this one's.
func TestUnionTypePartsSequenceAllocatesNothing(test *testing.T) {
	plain := new(checker.Type)

	sliced := testing.AllocsPerRun(100, func() {
		for _, part := range UnionTypeParts(plain) {
			unionTypePartsSink = part
		}
	})
	if sliced < 1 {
		test.Fatalf("UnionTypeParts allocated %.0f times per call; the comparison below cannot fail", sliced)
	}

	iterated := testing.AllocsPerRun(100, func() {
		for part := range UnionTypePartsSequence(plain) {
			unionTypePartsSink = part
		}
	})
	if iterated != 0 {
		test.Errorf("a range loop over UnionTypePartsSeq allocated %.0f times per call, want 0", iterated)
	}
	if unionTypePartsSink != plain {
		test.Error("the loop did not yield the type itself")
	}
}
