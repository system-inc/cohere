// Package parity holds every place `ST1011` reads a name being declared, each found and each near miss, for the
// original and the port to judge alike.
package parity

import "time"

// durationAlias is time.Duration under another name, which the check reads through.
type durationAlias = time.Duration

// ownDuration is a type of its own, whose underlying type is time.Duration, which the check leaves.
type ownDuration time.Duration

// A field of each shape: the duration, a pointer to it, the alias, and the near misses.
type settings struct {
	timeoutMS        time.Duration
	retrySecs        *time.Duration
	pollMillis       durationAlias
	idleSeconds      ownDuration
	nestedMsec       **time.Duration
	countMS          int
	firstMs, lastSec time.Duration
	plainTimeout     time.Duration
	lowercasems      time.Duration
	durations        []time.Duration
}

// A var and a const at package scope, each a specification the check reads.
var defaultWaitUsec = time.Microsecond

const tickMilliseconds = time.Millisecond

const (
	graceSec          = 2 * time.Second
	untypedSeconds    = 2
	typedMicroseconds = time.Duration(3)
	stringMilli       = "3"
	aliasedUsecs      = durationAlias(4)
)

var aliasedPointerMilli = (*durationAlias)(nil)

// waiter's method parameters are a field list the check reads.
type waiter interface {
	Wait(limitMS time.Duration) (spentMs time.Duration)
}

// handler's parameters are read in a function type too.
type handler func(delayMsecs time.Duration, attempts int)

// Parameters, results and type parameters, each a field list.
func declare[DelayMS any](waitMS time.Duration, startSecs *time.Duration) (elapsedMillis time.Duration, unused DelayMS) {
	var localUsecs time.Duration
	var (
		groupedMsecs, groupedSec time.Duration
		inferredMS               = time.Hour
	)
	shortMS := time.Second
	pointerSecs := &shortMS
	stringMS := "not a duration"
	shortMS, freshMilli := time.Minute, time.Minute
	values := map[string]time.Duration{}
	values["entryMS"] = time.Second
	lookupMS, found := values["entryMS"]
	if branchSec := time.Second; branchSec > 0 {
	}
	for loopMS := time.Duration(0); loopMS < time.Second; loopMS++ {
	}
	for _, rangedMS := range []time.Duration{time.Second} {
		_ = rangedMS
	}
	callback := func(innerMilliseconds time.Duration) {}
	var anything any = time.Second
	switch switchedMS := anything.(type) {
	case time.Duration:
		_ = switchedMS
	}
	_, _, _, _, _, _, _, _, _, _ = localUsecs, groupedMsecs, groupedSec, inferredMS, pointerSecs, stringMS, freshMilli,
		lookupMS, found, callback
	return waitMS + *startSecs, unused
}

// A method's receiver is a field list too, though its type is never time.Duration.
func (receiverMS settings) method(timeoutMs durationAlias) {}
