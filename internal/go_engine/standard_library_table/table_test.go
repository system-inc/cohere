package standard_library_table

import "testing"

// Each binary's line reads back as the table it says it carries, and only a stamped binary with bytes reads as
// carrying one, as the engine reads a table only then (#ccqee0e).
func TestTheVersionLineReadsBackAsTheTableItNames(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		stamp    string
		size     int
		carries  bool
		readBack string
	}{
		{"v0.2.0+3f1c", 4096, true, "v0.2.0+3f1c"},
		{"v0.2.0+3f1c", 0, false, ""},
		{"", 4096, false, ""},
		{"", 0, false, ""},
	} {
		output := "cohere dev\n  swift contract: 3\n  " + VersionLine(testCase.stamp, testCase.size) + "\n"
		stamp, size, carries, found := ReadVersionLine(output)
		if !found || carries != testCase.carries || stamp != testCase.readBack || carries && size != testCase.size {
			test.Errorf("stamp %q, %d bytes reads back as %q, %d bytes, carrying %t, want %q, carrying %t",
				testCase.stamp, testCase.size, stamp, size, carries, testCase.readBack, testCase.carries)
		}
	}
	if _, _, carries, found := ReadVersionLine("cohere 0.1.0\n  swift contract: 3\n"); carries || found {
		test.Errorf("a version with no table line reads as found %t, carrying %t, want neither", found, carries)
	}
}
