// Package kinds holds, for the facts' reach (#5vbs1np, #73k3wge), a pair of bodies for each fact kind a build
// compares: the user of a callee whose answer of that kind differs on windows, which leaves the standing set, and the
// user of a callee whose answer is the same in every build, which stands wherever the build can ask the kind again.
// Triviality and the stopping calls are not registered with the resolver (fact_resolver.Register), so the build reads
// every answer of theirs as unknown, and both their users leave (#73k3wge). Each callee names something each
// platform's file declares, so the reach at body grain lints it and the build asks its answers again; each user names
// only its callee, whose signature is the same in every build, so the reach alone would leave the user standing. Below
// them are the order rule's three shapes (#p018fh3), each named before any answer is asked, so their users never stand.
package kinds

import (
	"os"
	"testing"
)

// anything is any by another name, so the odd slice callees' values are no candidate of consistency-no-any, whose
// presence bound would take their users out before any fact could (#xeafx79), and so are any to every fact here.
type anything = any

// failure is an error whose nil pointer is never a nil error.
type failure struct{}

// Error says what failed.
func (*failure) Error() string {
	return "failure"
}

// Purity. size returns what helper returns of 4, so size is pure where helper is, on windows alone. It names only helper,
// whose signature is the same in every build, so the reach leaves size standing too, and only the facts take it out.
func size() int {
	return helper(4)
}

// helper returns platformSize's double, which counts its calls on every platform but windows, so helper is pure on
// windows alone. No body asks helper's purity: size's judgment reads it from the kind's own state, so the host holds
// no record of it, and only that a body the build lints holds no record takes size out (#5vbs1np, must 1).
func helper(base int) int {
	return platformSize(base)
}

// Warm discards size's count, a finding only where size is pure, on windows: the reach review's must 6 (#w72zxtq).
func Warm() int {
	size()
	return 1
}

// Warmer discards Warm's result, so Warm's own purity is asked, which differs on windows as size's does.
func Warmer() {
	Warm()
}

// width doubles base as a platformInteger, whose underlying type is each build's own, so the reach lints it, pure in
// every build.
func width(base int) int {
	return int(platformInteger(base)) * 2
}

// PurityStable discards width's double, a finding in every build.
func PurityStable() int {
	width(3)
	return 1
}

// No-return. halt calls platformExit, which exits on every platform but windows, so halt returns on windows alone.
func halt() {
	platformExit()
}

// NoReturnUser calls halt, whose no-return fact differs on windows.
func NoReturnUser() string {
	halt()
	return "after"
}

// haltAlways exits in every build, after naming the platform.
func haltAlways() {
	println(platformName())
	os.Exit(2)
}

// NoReturnStable calls haltAlways, whose no-return fact is the same in every build.
func NoReturnStable() string {
	haltAlways()
	return "after"
}

// Nil-ness. wrapped returns platformError's error, never nil on windows alone, where it holds a nil pointer.
func wrapped() error {
	return platformError()
}

// NilnessUser compares wrapped's error with nil, whose nil-ness differs on windows.
func NilnessUser() bool {
	return wrapped() != nil
}

// stable returns a failure, never nil, in every build, after naming the platform.
func stable() error {
	println(platformName())
	return &failure{}
}

// NilnessStable compares stable's error with nil, whose nil-ness is the same in every build.
func NilnessStable() bool {
	return stable() != nil
}

// Odd slice. pairs passes its values to platformPairs, which needs them even on every platform but windows.
func pairs(values ...anything) {
	platformPairs(values)
}

// OddUser hands pairs three values, a finding where pairs needs them even.
func OddUser() {
	pairs(1, 2, 3)
}

// pairsAlways needs its values even in every build, after naming the platform.
func pairsAlways(values ...anything) {
	println(platformName())
	if len(values)%2 != 0 {
		panic("odd")
	}
}

// OddStable hands pairsAlways three values, a finding in every build.
func OddStable() {
	pairsAlways(1, 2, 3)
}

// Triviality. typedNil returns a nil *failure after platformWork, which does real work on every platform but windows,
// so typedNil is trivial on windows alone.
func typedNil() error {
	platformWork()
	var value *failure
	return value
}

// TrivialUser compares typedNil's error with nil, a finding only where typedNil is not trivial.
func TrivialUser() bool {
	return typedNil() != nil
}

// typedNilAlways returns a nil *failure, trivial in every build. It converts to platformInteger, whose underlying type
// is each build's own, so the reach lints it, and it calls nothing, so its answer names no platform's file.
func typedNilAlways() error {
	var value *failure
	_ = platformInteger(0)
	return value
}

// TrivialStable compares typedNilAlways's error with nil, never a finding, since typedNilAlways is trivial.
func TrivialStable() bool {
	return typedNilAlways() != nil
}

// current is the test the stopping calls' pair reads, kept out of every signature: testing.T's layout is each build's
// own, so a body whose signature holds one is linted whole.
var current *testing.T

// Stopping calls. stopVia calls FailNow on platformTest's result, the test itself on every platform but windows, and on
// windows a stand-in whose FailNow does not stop the test.
func stopVia() {
	platformTest(current).FailNow()
}

// StoppingUser starts stopVia in a goroutine, a finding where stopVia stops the test.
func StoppingUser() {
	go stopVia()
}

// stopAlways stops the test in every build, after logging the platform.
func stopAlways() {
	current.Log(platformName())
	current.FailNow()
}

// StoppingStable starts stopAlways in a goroutine, a finding in every build.
func StoppingStable() {
	go stopAlways()
}

// lastFailure holds what cycleTail last returned.
var lastFailure error

// The order rule's cycle. cycleHead and cycleTail call each other, and cycleHead, which allocates, is never nil, more
// than the defaults of its signature, so which one a walk enters first decides what cycleTail returns.
func cycleHead(depth int) error {
	if depth > 0 {
		lastFailure = cycleTail(depth - 1)
	}
	return &failure{}
}

// cycleTail returns cycleHead's error.
func cycleTail(depth int) error {
	return cycleHead(depth)
}

// CycleUser compares cycleHead's error with nil.
func CycleUser() bool {
	return cycleHead(2) != nil
}

// The order rule's instantiation. viaInstance returns what makeFailure[int] returns, through the instantiation, which
// stands for makeFailure, declared after it.
func viaInstance() error {
	return makeFailure[int]()
}

// makeFailure is never nil.
func makeFailure[T any]() error {
	return &failure{}
}

// InstanceUser compares viaInstance's error with nil.
func InstanceUser() bool {
	return viaInstance() != nil
}

// node is a link of a list.
type node struct {
	next *node
}

// The order rule's method wrapper. viaWrapper returns what fresh returns, through a method value, whose wrapper stands
// for fresh, declared after it.
func viaWrapper(link *node) error {
	fresh := link.fresh
	return fresh()
}

// fresh is never nil.
func (link *node) fresh() error {
	return &failure{}
}

// WrapperUser compares viaWrapper's error with nil.
func WrapperUser() bool {
	return viaWrapper(&node{}) != nil
}
