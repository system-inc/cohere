package hir

import (
	"testing"
)

// TestFindLastUsageRecordsTheHighestOrder is the property the merge condition reads.
//
// A declaration read at orders 3, 9 and 5 must answer 9. Getting max wrong in the direction of
// "last write wins" produces 5 here, which is lower than the true last usage -- and a value that
// looks settled earlier than it is gets its scope merged forward past a read that is still live.
func TestFindLastUsageRecordsTheHighestOrder(t *testing.T) {
	// `total` is read in the loop body and again in the return, so its last usage must be the
	// return rather than the loop.
	function, _ := rangesFor(t, `
		function f(items) {
			let total = 0;
			for (const item of items) {
				total = total + item;
			}
			return total;
		}
	`)
	if function == nil {
		t.Fatal("the source did not lower")
	}
	tree, _ := BuildReactiveFunction(function)
	if tree == nil {
		t.Fatal("no tree was built")
	}

	usage := FindLastUsage(tree, function)
	if usage.Len() == 0 {
		t.Fatal("no declaration was recorded, so every assertion below is vacuous")
	}

	// Asserted against an independent walk rather than against the builder: the maximum order any
	// place carries must equal the maximum this table holds, or the walk missed a read.
	highestSeen := EvaluationOrder(0)
	VisitReactiveFunction(tree, ReactiveVisitor{
		Place: func(order EvaluationOrder, place Place, role PlaceRole) {
			if order > highestSeen {
				highestSeen = order
			}
		},
	})

	highestRecorded := EvaluationOrder(0)
	for declaration := range usage.byDeclaration {
		if at, found := usage.LastUsedAt(declaration); found && at > highestRecorded {
			highestRecorded = at
		}
	}
	if highestSeen == 0 {
		t.Fatal("the walk saw no place at a nonzero order; this fixture cannot discriminate")
	}
	if highestRecorded != highestSeen {
		t.Errorf("the highest order any place carries is %d and the table's highest is %d; the "+
			"table is not recording the last usage", highestSeen, highestRecorded)
	}
}

// TestLastUsedAtReportsMissesRatherThanZero pins the second return value.
//
// Upstream indexes its map with a non-null assertion and would throw on a miss. A linter cannot
// throw, and the two callers treat a miss differently, so a miss must be distinguishable from a
// real order of zero. Collapsing the two would make an unread declaration look like one last used
// at the very start of the function, which is exactly the answer that permits a merge.
func TestLastUsedAtReportsMissesRatherThanZero(t *testing.T) {
	usage := &LastUsage{byDeclaration: map[DeclarationId]EvaluationOrder{7: 0}}

	if order, found := usage.LastUsedAt(7); !found || order != 0 {
		t.Errorf("a declaration recorded at order zero answered (%d, %t), want (0, true)", order, found)
	}
	if _, found := usage.LastUsedAt(99); found {
		t.Error("an unrecorded declaration answered found; a miss must be distinguishable from a " +
			"real order of zero, because zero is the answer that permits a merge")
	}

	var nilUsage *LastUsage
	if _, found := nilUsage.LastUsedAt(7); found {
		t.Error("a nil table answered found")
	}
}

// TestAreEqualDependenciesIsSetEquality covers the comparison the merge turns on.
//
// Order must not matter -- it is an artifact of collection -- and the optional flag must, because
// `a?.b` and `a.b` invalidate under different conditions.
func TestAreEqualDependenciesIsSetEquality(t *testing.T) {
	plain := func(identifier IdentifierId, property string, optional bool) ReactiveScopeDependency {
		return ReactiveScopeDependency{
			Identifier: identifier,
			Path:       []DependencyPathEntry{{Property: property, Optional: optional}},
		}
	}

	for _, testCase := range []struct {
		name string
		a, b []ReactiveScopeDependency
		want bool
	}{
		{
			name: "same set in a different order",
			a:    []ReactiveScopeDependency{plain(1, "a", false), plain(2, "b", false)},
			b:    []ReactiveScopeDependency{plain(2, "b", false), plain(1, "a", false)},
			want: true,
		},
		{
			name: "different lengths",
			a:    []ReactiveScopeDependency{plain(1, "a", false)},
			b:    []ReactiveScopeDependency{plain(1, "a", false), plain(2, "b", false)},
			want: false,
		},
		{
			name: "different identifier",
			a:    []ReactiveScopeDependency{plain(1, "a", false)},
			b:    []ReactiveScopeDependency{plain(2, "a", false)},
			want: false,
		},
		{
			name: "different property",
			a:    []ReactiveScopeDependency{plain(1, "a", false)},
			b:    []ReactiveScopeDependency{plain(1, "b", false)},
			want: false,
		},
		{
			name: "optional differs, which is a real difference",
			a:    []ReactiveScopeDependency{plain(1, "a", false)},
			b:    []ReactiveScopeDependency{plain(1, "a", true)},
			want: false,
		},
		{
			name: "both empty",
			a:    nil,
			b:    nil,
			want: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := AreEqualDependencies(testCase.a, testCase.b); got != testCase.want {
				t.Errorf("got %t, want %t", got, testCase.want)
			}
			// Symmetry is asserted rather than assumed: the implementation walks one side looking
			// into the other, and a length check is the only thing making that symmetric.
			if got := AreEqualDependencies(testCase.b, testCase.a); got != testCase.want {
				t.Errorf("reversed: got %t, want %t", got, testCase.want)
			}
		})
	}
}

// TestAreLValuesLastUsedByScopeDeclinesOnMissingInformation pins the conservative direction.
//
// A declaration with no recorded usage is not a declaration proven safe to merge, it is one this
// pass knows nothing about. Upstream would throw; answering true instead would permit a merge on
// absent information, which is the wrong direction for a pass whose true answer relaxes a guard.
func TestAreLValuesLastUsedByScopeDeclinesOnMissingInformation(t *testing.T) {
	usage := &LastUsage{byDeclaration: map[DeclarationId]EvaluationOrder{
		1: 5,
		2: 12,
	}}

	if !AreLValuesLastUsedByScope(10, []DeclarationId{1}, usage) {
		t.Error("a declaration last used at 5 before a scope ending at 10 should be mergeable")
	}
	if AreLValuesLastUsedByScope(10, []DeclarationId{2}, usage) {
		t.Error("a declaration last used at 12 is read after a scope ending at 10, so the merge " +
			"must be declined")
	}
	if AreLValuesLastUsedByScope(10, []DeclarationId{1, 2}, usage) {
		t.Error("one unmergeable lvalue must decline the whole set")
	}
	if AreLValuesLastUsedByScope(10, []DeclarationId{99}, usage) {
		t.Error("an unrecorded declaration answered true; missing information must decline")
	}
	if !AreLValuesLastUsedByScope(10, nil, usage) {
		t.Error("an empty lvalue set has nothing blocking the merge and should answer true")
	}
	// The boundary: upstream's test is `lastUsedAt >= scope.range.end`, so a usage exactly at the
	// end is a usage after the scope.
	if AreLValuesLastUsedByScope(5, []DeclarationId{1}, usage) {
		t.Error("a declaration last used exactly at the scope end must decline; upstream's test " +
			"is >= rather than >")
	}
}

// TestFindLastUsageCorpus runs the walk over real source.
//
// The property asserted is total coverage: every declaration any place names must appear in the
// table, because a missing entry makes `AreLValuesLastUsedByScope` decline a merge that should have
// been allowed, and that failure is silent -- the output is merely more granular.
func TestFindLastUsageCorpus(t *testing.T) {
	functions, declarationsSeen, missing, outOfOrder := 0, 0, 0, 0
	// Accumulated across the whole corpus rather than per function, because a DeclarationId is only
	// unique within its function -- so these are compared per function, inside the walk below, and
	// only the last function's tables survive to the assertion. That is deliberate: the property is
	// per function and one counterexample is enough.
	highestPerDeclaration := map[DeclarationId]EvaluationOrder{}
	recordedTable := map[DeclarationId]EvaluationOrder{}

	forEachCorpusFunction(t, 200, func(function *Function, ranges *MutableRanges, scopes *ReactiveScopes) {
		aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
		BuildReactiveScopeTerminals(function, scopes, MergedScopeIdentity{Aligned: aligned, Merged: merged})

		tree, _ := BuildReactiveFunction(function)
		if tree == nil {
			return
		}
		functions++
		usage := FindLastUsage(tree, function)

		highest := map[DeclarationId]EvaluationOrder{}
		previous, first := EvaluationOrder(0), true
		VisitReactiveFunction(tree, ReactiveVisitor{
			Place: func(order EvaluationOrder, place Place, role PlaceRole) {
				declarationsSeen++
				if !first && order < previous {
					outOfOrder++
				}
				previous, first = order, false

				declaration := declarationOf(function, place.Identifier)
				if _, found := usage.LastUsedAt(declaration); !found {
					missing++
				}
				if held, seen := highest[declaration]; !seen || order > held {
					highest[declaration] = order
				}
			},
		})

		// Keep the tables of any function whose walk was non-monotonic, since those are the only
		// ones where max and last-write can differ.
		if outOfOrder > 0 && len(highestPerDeclaration) == 0 {
			for declaration, order := range highest {
				highestPerDeclaration[declaration] = order
				if at, found := usage.LastUsedAt(declaration); found {
					recordedTable[declaration] = at
				}
			}
		}
	})

	if functions < 50 {
		t.Fatalf("only %d functions converted; the corpus walk is not reaching real source", functions)
	}
	if declarationsSeen == 0 {
		t.Fatal("the walk saw no places, so a zero miss count proves nothing")
	}
	if missing != 0 {
		t.Errorf("%d of %d place visits named a declaration absent from the table; the walk that "+
			"builds it and the walk that reads it disagree", missing, declarationsSeen)
	}

	// # The assertion that actually discriminates max from last-write
	//
	// The hand-written case above cannot: it only checks that the table's highest entry matches the
	// walk's highest order, which a last-write-wins implementation also satisfies. A mutation
	// replacing `order > previous` with an unconditional overwrite survived it.
	//
	// It survives only if the walk is monotonic, and it is not: 473 of 26,331 place visits over 100
	// corpus files arrive at a LOWER order than the visit before them. So a last-write table is
	// genuinely wrong on those, and the property that separates the two is stated directly -- every
	// recorded entry must be the maximum over all visits naming that declaration, not the last one.
	if outOfOrder == 0 {
		t.Fatal("no place visit arrived out of order, so a last-write table would be identical to " +
			"a max table here and this assertion cannot discriminate; 473 of 26,331 were measured")
	}
	for declaration, recorded := range highestPerDeclaration {
		if held, found := recordedTable[declaration]; !found || held != recorded {
			t.Errorf("declaration %d was seen at a highest order of %d and the table holds %d; "+
				"the table is recording the last usage rather than the greatest",
				declaration, recorded, held)
			break
		}
	}

	t.Logf("functions=%d placeVisits=%d missing=%d outOfOrder=%d",
		functions, declarationsSeen, missing, outOfOrder)
}
