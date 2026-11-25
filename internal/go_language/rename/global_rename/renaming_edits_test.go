package global_rename

import "testing"

// A comment respelling a renamed name skips every directive, whose text a tool reads exactly: an embed pattern, a
// linkname, a cgo export, a line directive, a generator's arguments and cohere's own. A prose comment is respelled.
func TestACommentRespellingSkipsEveryDirective(test *testing.T) {
	test.Parallel()
	for _, directive := range []string{
		"//go:embed cfg/*", "//go:linkname cfg runtime.cfg", "//export cfg", "//line cfg.go:1",
		"//go:generate stringer -type=cfg", "//cohere:ignore consistency-no-abbreviated-identifier cfg is upstream's",
	} {
		if indexes := commentIndexes(directive, "cfg"); len(indexes) != 0 {
			test.Errorf("%q would be respelled at %v", directive, indexes)
		}
	}
	if indexes := commentIndexes("// cfg holds the settings.", "cfg"); len(indexes) != 1 {
		test.Errorf("a prose comment's cfg was found at %v, want once", indexes)
	}
}
