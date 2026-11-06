package nextjs

import "testing"

// Each App Router convention is told by its punctuation, and a folder that only resembles one, or a private folder
// whose underscore opts it out of routing, is the project's own name.
func TestIsRoutingFolder(test *testing.T) {
	test.Parallel()
	cases := []struct {
		folder string
		want   bool
	}{
		{"[id]", true},
		{"[entityKey]", true},
		{"[...notFound]", true},
		{"[[...slug]]", true},
		{"(os-layout)", true},
		{"(mainlayout)", true},
		{"(.)photo", true},
		{"(..)photo", true},
		{"(..)(..)photo", true},
		{"(...)photo", true},
		{"@modal", true},
		{"_components", false},
		{"components", false},
		{"[unclosed", false},
		{"closed]", false},
		{"profile-images", false},
		{"", false},
	}
	for _, testCase := range cases {
		if got := IsRoutingFolder(testCase.folder); got != testCase.want {
			test.Errorf("IsRoutingFolder(%q) = %t, want %t", testCase.folder, got, testCase.want)
		}
	}
}
