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
		// A release's stamp as the release writes it, spaces and all (#ccqee0e).
		{"0.2.0 994d39a3d2936e4283b0de5958c088dd99d93eee go1.27.0", 2145372, true,
			"0.2.0 994d39a3d2936e4283b0de5958c088dd99d93eee go1.27.0"},
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
	// A line it cannot read says nothing, as no line does, so a change to the line's shape fails closed and never reads
	// a release as a local build (#ccqee0e).
	for _, unreadable := range []string{
		VersionPrefix + "release 0.2.0 994d39a3 go1.27.0",
		VersionPrefix + "release 0.2.0 994d39a3 go1.27.0, many bytes",
		VersionPrefix + "release 0.2.0 994d39a3 go1.27.0, 2145372",
		VersionPrefix + "embedded, 2145372 bytes",
	} {
		if _, _, carries, found := ReadVersionLine("cohere 0.2.0\n  " + unreadable + "\n"); carries || found {
			test.Errorf("the unreadable line %q reads as found %t, carrying %t, want neither", unreadable, found,
				carries)
		}
	}
}
