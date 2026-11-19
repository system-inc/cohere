package timeline

import (
	"encoding/json"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// countedClock is a clock whose wall steps a millisecond and whose CPU steps a quarter of one in user and an eighth in
// the kernel at each read, every read counted, and whose CPU reads known as known says.
type countedClock struct {
	start time.Time
	reads atomic.Int64
	known bool
}

// clock is the Clock reading counted.
func (counted *countedClock) clock() Clock {
	return Clock{
		Now: func() time.Time {
			return counted.start.Add(time.Duration(counted.reads.Add(1)) * time.Millisecond)
		},
		ProcessTimes: func() (time.Duration, time.Duration, bool) {
			read := time.Duration(counted.reads.Add(1))
			return read * 250 * time.Microsecond, read * 125 * time.Microsecond, counted.known
		},
	}
}

// A nil recorder is a run that did not ask: every method does nothing and reads no clock, its hook is nil, which Tell
// reads as nothing to stamp, a build's name is never asked, so no label is built, and its record is the zero record.
func TestANilRecorderDoesNothingAndReadsNoClock(test *testing.T) {
	test.Parallel()
	var recorder *Recorder
	recorder.Stamp("engine.main.begin")
	asked := &atomic.Int64{}
	if hook := recorder.Hook(countedName{asked: asked}, 0, ""); hook != nil {
		test.Error("a nil recorder handed a hook, which would stamp a run that did not ask")
	}
	if asked.Load() != 0 {
		test.Errorf("a nil recorder asked a build's name %d times, want none", asked.Load())
	}
	Tell(recorder.Hook(nil, 0, "host."), "listing.end")
	if record := recorder.Record(true); record.Timed || record.Stamps != nil || record.Unavailable != "" {
		test.Errorf("a nil recorder's record is %+v, want the zero record", record)
	}
}

// A recorder keeps each stamp in arrival order, a hook's under its build, its round and its prefix, each with its wall
// since the start and the process's user and system CPU apart, and writes timed only when told.
func TestARecorderKeepsEachStampInOrderWithItsWallAndCpu(test *testing.T) {
	test.Parallel()
	counted := &countedClock{start: time.Now(), known: true}
	recorder := New(counted.start, counted.clock())
	recorder.Stamp("engine.main.begin")
	Tell(recorder.Hook(countedName{asked: &atomic.Int64{}}, 1, ""), "gate.taken")
	record := recorder.Record(false)
	if record.Timed || record.Unavailable != "" {
		test.Errorf("the record is timed %v, unavailable %q, want neither", record.Timed, record.Unavailable)
	}
	want := []contract.TimelineStamp{
		{Label: "engine.main.begin", WallMicroseconds: 1000, UserMicroseconds: 500, SystemMicroseconds: 250},
		{Build: "windows/amd64", Round: 1, Label: "gate.taken", WallMicroseconds: 3000, UserMicroseconds: 1000,
			SystemMicroseconds: 500},
	}
	if !slices.Equal(record.Stamps, want) {
		test.Errorf("the stamps are %+v, want %+v", record.Stamps, want)
	}
	if !recorder.Record(true).Timed {
		test.Error("a record of a timed run did not say so")
	}
}

// A recorder whose CPU could not be read once writes no CPU figure at all, every stamp's 0, and says why, while every
// stamp's wall stays.
func TestARecorderWithoutACpuClockSaysWhy(test *testing.T) {
	test.Parallel()
	counted := &countedClock{start: time.Now()}
	recorder := New(counted.start, counted.clock())
	recorder.Stamp("engine.main.begin")
	record := recorder.Record(false)
	if record.Unavailable == "" || len(record.Stamps) != 1 {
		test.Fatalf("the record is %+v, want one stamp and a reason", record)
	}
	if stamp := record.Stamps[0]; stamp.WallMicroseconds != 1000 || stamp.UserMicroseconds != 0 ||
		stamp.SystemMicroseconds != 0 {
		test.Errorf("the stamp is %+v, want its wall and no CPU", stamp)
	}
}

// Stamps from many goroutines at once are each kept once, and their walls never go back in the order they were kept.
func TestStampsFromManyGoroutinesKeepTheirWallInOrder(test *testing.T) {
	test.Parallel()
	recorder := New(time.Now(), SystemClock())
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 100 {
				recorder.Stamp("a")
			}
		}()
	}
	for range 8 {
		<-done
	}
	stamps := recorder.Record(false).Stamps
	if len(stamps) != 800 {
		test.Fatalf("%d stamps were kept, want 800", len(stamps))
	}
	for index := 1; index < len(stamps); index++ {
		if stamps[index].WallMicroseconds < stamps[index-1].WallMicroseconds {
			test.Fatalf("stamp %d's wall %d is before the one kept before it, %d", index,
				stamps[index].WallMicroseconds, stamps[index-1].WallMicroseconds)
		}
	}
	encoded, encodeError := json.Marshal(recorder.Record(false))
	if encodeError != nil || !json.Valid(encoded) {
		test.Fatalf("the record does not encode: %v", encodeError)
	}
}
