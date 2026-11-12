package check

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// A record measured under one word size and maximum alignment is declined as unknown by a build measured under others,
// or by none, before any read is compared (#pcv6eff). The name user reads a constant, which renders alike under any
// sizes, so compared it would hold; the stable user reads a struct with an unexported int, which renders apart under
// 386's sizes, so compared it would decline as rendering differently. Both hold in a build measured as the record was,
// so the record is judged there, and only the sizes decline it.
func TestPersistedReadsMeasuredUnderOtherSizesAreDeclined(test *testing.T) {
	test.Parallel()

	host := measuredReadsBuild()
	darwin := checkedWorld(test, "darwin", "")
	closure := map[string]*types.Package{verdictPlatform: darwin.platform}
	unmeasuredDeclined := "unknown: its reads were measured under word size 8, maximum alignment 8, and the build is " +
		"measured by no sizes"
	for _, user := range []string{"name", "stable"} {
		restoring, _ := hostReads(test, user)
		persisted := persistedRestoring(test, restoring, host, ReadsRendererVersion)
		if holds, why := TypesHold(persisted, closure, nil, host, measuredReadsBuild()); !holds {
			test.Fatalf("the %s user's record does not hold in a build measured as it was: %s", user, why)
		}
		other := NewReadsBuild()
		other.measureBy(otherSizes)
		for _, step := range []struct {
			name  string
			build *ReadsBuild
			want  string
		}{
			{"measured by 386's sizes", other, otherSizesDeclined},
			{"measured by no sizes", NewReadsBuild(), unmeasuredDeclined},
		} {
			holds, why := TypesHold(persisted, closure, nil, host, step.build)
			if holds || why != step.want || reasonWord(why) != declineKeys["unknown"] {
				test.Errorf("the %s user in a build %s: want declined, %q, counted as %s, got %t, %q, counted as %s",
					user, step.name, step.want, declineKeys["unknown"], holds, why, reasonWord(why))
			}
		}
	}
}

// A record that names something it does not hold is declined as unreadable, never read past its end (#pcv6eff): each
// kind of reference, past what the record holds or zero where a read must name one, read from the record and from
// its bytes as a table or a store holds them, declines the package at its types' verdict and at its facts', counted
// as readsUnreadable, and so do bytes gob refuses. The record they are made from is readable, and judged without one.
func TestUnreadablePersistedReadsAreDeclined(test *testing.T) {
	test.Parallel()

	readable := func() persistedReads {
		return persistedReads{
			Renderer:         ReadsRendererVersion,
			WordSize:         8,
			MaximumAlignment: 8,
			Owners:           []string{verdictPlatform},
			FactTypes:        []string{factTypeName(reflect.TypeFor[*valueFact]())},
			Objects:          []persistedObject{{Owner: 1, Path: "NewStable"}},
			Uses:             []persistedUse{{Object: 1, Kind: build_reads.UseMember}},
			Selections:       []persistedSelection{{Receiver: 1, Name: "NewStable", Selected: 1}},
			Facts:            []persistedFact{{Owner: 1, Object: 1}},
			Lookups: []persistedLookup{
				{Query: build_reads.QueryScope, Path: 1, Name: "NewStable", Found: true, Answer: 1},
				{Query: build_reads.QueryDeprecated, Path: 1, Subject: 1},
				{Query: build_reads.QueryMethod, Name: "Close", Candidates: []int{1},
					RuleCandidates: []persistedMethodCandidate{{TypeName: 1}}},
			},
		}
	}
	host := measuredReadsBuild()
	linux := linuxBuild(test, nil)
	linux.build.measureBy(persistedSizes)
	allFactTypes := map[reflect.Type]bool{reflect.TypeFor[*valueFact](): true, deprecatedFactType: true}
	// judge is both halves of the verdict on held, each on its own, with a panic, which an accessor reading past the
	// record raises, returned as the reason.
	judge := func(held *heldReads) [2]string {
		restoring := Restoring{Path: verdictUser, Imports: []string{verdictPlatform}, persisted: held}
		var reasons [2]string
		for half, decide := range []func() (bool, string){
			func() (bool, string) { return TypesHold(restoring, linux.closure, nil, host, linux.build) },
			func() (bool, string) {
				ReadFactTypes(restoring)
				return FactsHold(restoring, linux.closure, host, linux.build, linux.fact, allFactTypes)
			},
		} {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						reasons[half] = fmt.Sprintf("panicked: %v", recovered)
					}
				}()
				holds, why := decide()
				if holds {
					why = "held"
				}
				reasons[half] = why
			}()
		}
		return reasons
	}
	control := newHeldReads(readable(), persistedLearned)
	if control.unreadable != "" {
		test.Fatalf("the readable record reads as unreadable: %s", control.unreadable)
	}
	for _, reason := range judge(control) {
		if strings.HasPrefix(reason, "unreadable") || strings.HasPrefix(reason, "panicked") {
			test.Fatalf("the readable record is judged as %q", reason)
		}
	}
	for _, step := range []struct {
		what   string
		damage func(record *persistedReads)
	}{
		{"object's owner", func(record *persistedReads) { record.Objects[0].Owner = 2 }},
		{"use's object", func(record *persistedReads) { record.Uses[0].Object = 0 }},
		{"use's object", func(record *persistedReads) { record.Uses[0].Object = 2 }},
		{"use's kind", func(record *persistedReads) { record.Uses[0].Kind = "Planted" }},
		{"selection's receiver", func(record *persistedReads) { record.Selections[0].Receiver = 0 }},
		{"selection's receiver", func(record *persistedReads) { record.Selections[0].Receiver = 2 }},
		{"selection's member", func(record *persistedReads) { record.Selections[0].Selected = 0 }},
		{"selection's member", func(record *persistedReads) { record.Selections[0].Selected = 2 }},
		{"fact read's owner", func(record *persistedReads) { record.Facts[0].Owner = 0 }},
		{"fact read's owner", func(record *persistedReads) { record.Facts[0].Owner = 2 }},
		{"fact read's object", func(record *persistedReads) { record.Facts[0].Object = 2 }},
		{"fact read's type", func(record *persistedReads) { record.Facts[0].FactType = 1 }},
		{"lookup's package", func(record *persistedReads) { record.Lookups[0].Path = 2 }},
		{"lookup's answer", func(record *persistedReads) { record.Lookups[0].Answer = 2 }},
		{"lookup's subject", func(record *persistedReads) { record.Lookups[1].Subject = 2 }},
		{"lookup's candidate", func(record *persistedReads) { record.Lookups[2].Candidates[0] = 0 }},
		{"lookup's candidate", func(record *persistedReads) { record.Lookups[2].Candidates[0] = 2 }},
		{"lookup's rule candidate", func(record *persistedReads) {
			record.Lookups[2].RuleCandidates[0].TypeName = 0
		}},
		{"lookup's rule candidate", func(record *persistedReads) {
			record.Lookups[2].RuleCandidates[0].TypeName = 2
		}},
	} {
		damaged := readable()
		step.damage(&damaged)
		encoded, encodeError := encodePersistedReads(damaged)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		for source, held := range map[string]*heldReads{
			"the record": newHeldReads(damaged, persistedLearned),
			"its bytes":  heldReadsOf(encoded, persistedLearned),
		} {
			for half, reason := range judge(held) {
				if !strings.HasPrefix(reason, "unreadable: its "+step.what+" is ") ||
					reasonWord(reason) != "readsUnreadable" {
					test.Errorf("%s with its %s damaged, %+v: want half %d declined as unreadable, counted as "+
						"readsUnreadable, got %q", source, step.what, damaged, half, reason)
				}
			}
		}
	}
	for half, reason := range judge(heldReadsOf([]byte("not a record"), persistedLearned)) {
		if !strings.HasPrefix(reason, "unreadable: ") || reasonWord(reason) != "readsUnreadable" {
			test.Errorf("bytes gob refuses: want half %d declined as unreadable, got %q", half, reason)
		}
	}
}
