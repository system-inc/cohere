package tailwind

import (
	"slices"
	"testing"
)

// TestCompareClassNamesIsUpstreamsCompare holds compareClassNames to upstream's own compare.test.ts at
// v4.3.3, case for case: the pairs by sign, and the three lists it sorts. The order decides which listed
// class a collapse is spelled as, `size-14` before `size-rail`, so a compare that read `10` before `2`
// would spell `p-2 p-10`'s neighbours wrongly.
func TestCompareClassNamesIsUpstreamsCompare(test *testing.T) {
	test.Parallel()
	sign := func(value int) int {
		switch {
		case value < 0:
			return -1
		case value > 0:
			return 1
		}
		return 0
	}
	for _, testCase := range []struct {
		left  string
		right string
		want  int
	}{
		{"abc", "abc", 0},
		{"abc", "abcd", -1},
		{"abcd", "abc", 1},
		{"1", "1", 0},
		{"1", "2", -1},
		{"2", "1", 1},
		{"1", "10", -1},
		{"10", "1", 1},
		{"75", "700", -1},
		{"700", "75", 1},
		{"75", "770", -1},
		{"770", "75", 1},
		// Beside upstream's: equal numbers spelled differently fall back to their text.
		{"0123", "123", -1},
	} {
		if got := sign(compareClassNames(testCase.left, testCase.right)); got != testCase.want {
			test.Errorf("compare(%q, %q) = %d, want %d", testCase.left, testCase.right, got, testCase.want)
		}
	}

	for _, sorted := range [][]string{
		{"p-0", "p-0.5", "p-1", "p-1.5", "p-2", "p-10", "p-12", "p-20", "p-21"},
		{
			"bg-red-50", "bg-red-50/50", "bg-red-50/60", "bg-red-50/70",
			"bg-red-500", "bg-red-500/50", "bg-red-500/60", "bg-red-500/70",
			"text-5xl", "text-6xl", "text-6xl/loose", "text-6xl/wide",
		},
		{
			"foo-1-bar-2-baz-3", "foo-12-bar-34-baz-4", "foo-12-bar-34-baz-45", "foo-12-bar-34-baz-456",
			"foo-123-bar-455-baz-789", "foo-123-bar-456-baz-788", "foo-123-bar-456-baz-789",
			"foo-123-bar-456-baz-790", "foo-123-bar-457-baz-789", "foo-124-bar-456-baz-788",
			"foo-125-bar-456-baz-790", "foo-126-bar-455-baz-789", "foo-127-bar-456-baz-789",
			"foo-128-bar-457-baz-789",
		},
		{"duration-75", "duration-150", "duration-700", "duration-1000", "duration-initial"},
	} {
		shuffled := slices.Clone(sorted)
		slices.Reverse(shuffled)
		slices.SortStableFunc(shuffled, compareClassNames)
		if !slices.Equal(shuffled, sorted) {
			test.Errorf("sorted as\n  %v\nwant\n  %v", shuffled, sorted)
		}
	}
}

// TestFirstInClassListOrder is the listed class a collapse is spelled as, among the classes that sign as
// the pair does, and the orders it refuses to guess.
func TestFirstInClassListOrder(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		matches     []string
		want        string
		wantOrdered bool
		why         string
	}{
		{
			matches: []string{"size-rail", "size-14"}, want: "size-14", wantOrdered: true,
			why: "a digit sorts before a letter, so a spacing key equal to a multiple loses to the multiple",
		},
		{
			matches: []string{"size-gutter", "size-5"}, want: "size-5", wantOrdered: true,
			why: "the same for the planted theme's other spacing key",
		},
		{
			matches: []string{"size-2/4", "size-1/2"}, want: "size-1/2", wantOrdered: true,
			why: "two fractions of one utility compare as numbers",
		},
		{
			matches: []string{"w-1/1", "w-full"}, want: "w-full", wantOrdered: true,
			why: "sortFractionsLast puts a utility's fractions after its other classes, whatever their names",
		},
		{
			matches: []string{"inset-x-1/2", "left-full"}, wantOrdered: false,
			why: "a fraction competing with another utility's class sits in a bucket this does not rebuild",
		},
		{
			matches: []string{"text-sm/6", "text-sm"}, wantOrdered: false,
			why: "upstream lists a class with a modifier right after its class, not where its name sorts",
		},
		{matches: nil, wantOrdered: false, why: "nothing listed signs alike"},
	} {
		got, ordered := firstInClassListOrder(testCase.matches)
		if ordered != testCase.wantOrdered || got != testCase.want {
			test.Errorf("%v gave %q (ordered %v), want %q (ordered %v): %s",
				testCase.matches, got, ordered, testCase.want, testCase.wantOrdered, testCase.why)
		}
	}
}
