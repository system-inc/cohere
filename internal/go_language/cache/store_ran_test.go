package cache

import (
	"os"
	"testing"
)

// A ran marker is kept once and outlives the packs (#s8e6z0n): it is missing until marked, there after, written only
// the first time, so a second mark leaves the file as it was, and another executable's sweep of the packs leaves it.
func TestARanMarkerOutlivesTheSweepOfThePacks(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir(), NewShardStamps(nil))
	if store.HasRun(samplePackPlace) {
		test.Fatal("an empty store holds a ran marker")
	}
	if writeError := store.WritePack(samplePackPlace, samplePackEntries, samplePackStamps); writeError != nil {
		test.Fatal(writeError)
	}
	if markError := store.MarkRan(samplePackPlace); markError != nil {
		test.Fatal(markError)
	}
	marked, statError := os.Stat(store.ranPath(samplePackPlace))
	if statError != nil || !store.HasRun(samplePackPlace) {
		test.Fatalf("the store holds no ran marker once marked: %v", statError)
	}
	if markError := store.MarkRan(samplePackPlace); markError != nil {
		test.Fatal(markError)
	}
	if again, againError := os.Stat(store.ranPath(samplePackPlace)); againError != nil ||
		!again.ModTime().Equal(marked.ModTime()) || !os.SameFile(again, marked) {
		test.Errorf("a second mark wrote the marker again: %v", againError)
	}
	store.RemoveOtherIdentitiesPacks(PackIdentity("another executable"))
	if store.HasPack(samplePackPlace) || !store.HasRun(samplePackPlace) {
		test.Errorf("after another executable's sweep, want the pack gone and the marker kept, got the pack %t and "+
			"the marker %t", store.HasPack(samplePackPlace), store.HasRun(samplePackPlace))
	}
}
