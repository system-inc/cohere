package parity

import (
	"sync"
	"sync/atomic"
)

// What holds a lock: sync's locks, sync/atomic's typed values, which carry their own noCopy, a lock of the
// package's own, embedded or held in a field, in arrays, and through aliases and defined types.

type embedded struct {
	sync.Mutex
}

type embeddedPointer struct {
	*sync.Mutex
}

type embeddedLocker struct {
	sync.Locker
}

type nested struct {
	inner embedded
}

type node struct {
	next  *node
	mutex sync.RWMutex
}

// noCopy is the convention vet reads in any package: a type whose pointer has Lock and Unlock.
type noCopy struct{}

func (*noCopy) Lock() {}

func (*noCopy) Unlock() {}

type guarded struct {
	_     noCopy
	value int
}

// valueLocker is a sync.Locker as a value, so copying it copies no lock.
type valueLocker struct{}

func (valueLocker) Lock() {}

func (valueLocker) Unlock() {}

type mutexAlias = sync.Mutex

type holderAlias = holder

type arrayAlias = [2]sync.Mutex

type localMutex sync.Mutex

type localHolder holder

type matrix [2][3]holder

type box[T any] struct {
	value T
}

type boxAlias[T any] = box[T]

func lockKinds(
	mutex sync.Mutex,
	readWrite sync.RWMutex,
	once sync.Once,
	group sync.WaitGroup,
	condition sync.Cond,
	pool sync.Pool,
	table sync.Map,
	counter atomic.Int64,
	flag atomic.Bool,
	pointer atomic.Pointer[int],
	value atomic.Value,
) {
}

func holders(
	first embedded,
	second embeddedPointer,
	third embeddedLocker,
	fourth nested,
	fifth node,
	sixth guarded,
	seventh valueLocker,
	eighth noCopy,
) {
}

func aliases(first mutexAlias, second holderAlias, third arrayAlias, fourth localMutex, fifth localHolder) {
}

func collections(
	first [2]sync.Mutex,
	second matrix,
	third []sync.Mutex,
	fourth map[string]sync.Mutex,
	fifth chan sync.Mutex,
	sixth func(sync.Mutex),
	seventh *[2]sync.Mutex,
	eighth struct{ mutex sync.Mutex },
	ninth [0]sync.Mutex,
) {
}

func instances(first box[sync.Mutex], second box[*sync.Mutex], third boxAlias[holder], fourth box[box[embedded]]) {
}

func (box[T]) anyElement() {}

// Constraints: exact and approximate terms, unions in both orders, intersections, embedded constraints, an empty
// type set, constraints that restrict nothing, and terms that are pointers, arrays and slices.

type lockLike interface {
	~struct{ mutex sync.Mutex } | sync.RWMutex
}

type numeric interface {
	~int | ~float64
}

func exact[T sync.Mutex](value T) {}

func approximate[T ~struct{ mutex sync.Mutex }](value T) {}

func unionLockFirst[T sync.Mutex | int](value T) {}

func unionLockLast[T int | string | holder](value T) {}

func unionTilde[T ~int | ~struct{ mutex sync.Mutex }](value T) {}

func embeddedConstraint[T lockLike](value T) {}

func intersection[T interface {
	lockLike
	sync.RWMutex
}](value T) {
}

func emptyIntersection[T interface {
	numeric
	sync.Mutex
}](value T) {
}

func emptyTypeSet[T interface {
	int
	string
}](value T) {
}

func unrestricted[T any, U comparable, V interface{ Lock() }](first T, second U, third V) {}

func pointerTerm[T *sync.Mutex | *holder](value T) {}

func arrayTerm[T [2]sync.Mutex](value T) {}

func sliceTerm[T ~[]sync.Mutex](value T) {}

// Interface terms of a union may overlap, so normalizing merges them, keeping the first's place and the wider term.
func mergedExactFirst[T interface{ sync.RWMutex } | lockLike](value T) {}

func mergedApproximateLast[T interface{ struct{ mutex sync.Mutex } } | interface{ ~struct{ mutex sync.Mutex } }](
	value T,
) {
}

func mergedApproximateFirst[T interface{ ~struct{ mutex sync.Mutex } } | interface{ struct{ mutex sync.Mutex } }](
	value T,
) {
}

func intersectedNarrower[T interface {
	~struct{ mutex sync.Mutex }
	struct{ mutex sync.Mutex }
}](value T) {
}

func withMethod[T interface {
	~struct{ mutex sync.Mutex }
	Method()
}](value T) {
}

func parameterArray[T sync.Mutex](values [2]T, pointer *T, slice []T) {}

func parameterInBox[T sync.Mutex](value box[T]) {}

func parameterInResult[T sync.Mutex]() (value T) { return }

func genericCopies[T sync.Mutex, U any](pointer *T, other *U) {
	copied := *pointer
	var declared = *pointer
	untouched := *other
	_, _, _ = copied, declared, untouched
}

type lockedBox[T sync.Locker] struct {
	locker T
}

func lockedBoxes(first lockedBox[*sync.Mutex], second lockedBox[valueLocker]) {}

// Tuples: each comma-ok read whose value holds a lock.
func tuples(table map[string]holder, channel chan sync.Mutex, value any) {
	fromTable, present := table["key"]
	received, open := <-channel
	single := <-channel
	asserted, isMutex := value.(sync.Mutex)
	assertedOnly := value.(sync.Mutex)
	var declared, alsoPresent = table["key"]
	_, _, _, _, _, _, _, _, _ = fromTable, present, received, open, single, asserted, isMutex, assertedOnly, declared
	_ = alsoPresent
}

// Conversions are calls, so a lock copied by one is not judged, as the original does not judge it.
func conversions(value holder, mutex sync.Mutex) {
	converted := localHolder(value)
	alsoConverted := sync.Mutex(mutex)
	_, _ = converted, alsoConverted
}

// A parenthesized function is no identifier, so the builtins the original lets through are judged when written so.
func parenthesizedCalls(mutexes [2]sync.Mutex, mutex sync.Mutex) {
	_ = (len)(mutexes)
	(takesAny)(mutex)
	(*receiver).method(nil, mutex)
}
