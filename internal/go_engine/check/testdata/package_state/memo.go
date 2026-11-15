// Package packagestate is the package-state guard's fixture (#rhsx4gc): state a package-level variable holds behind a
// pointer, written by its type's own methods, as rules/load_memo holds its memo. Each line whose write the guard must
// see ends with the marker the test reads; every other write is one it must leave alone.
package packagestate

// loadMemo is what the process keeps across every pass, by key.
type loadMemo struct {
	byLoad map[string]int
	asks   int
}

// newMemo is an empty memo.
func newMemo() *loadMemo {
	return &loadMemo{byLoad: map[string]int{}}
}

// memo is the process's one memo, held by a package-level variable its methods write through a receiver of its name.
var memo = newMemo()

// remember writes the memo's entry through the receiver, which the variable's name alone never sees.
func (memo *loadMemo) remember(key string, value int) {
	memo.byLoad[key] = value // written: package state
}

// forget writes the memo's map through a local it is copied into.
func (memo *loadMemo) forget(key string) {
	byLoad := memo.byLoad
	byLoad[key] = 0 // written: package state
}

// counted counts on a copy of the memo, its value receiver, which no other pass sees.
func (memo loadMemo) counted() int {
	memo.asks++
	return memo.asks
}

// scratch is space one call makes for itself, which no package-level variable holds.
type scratch struct {
	seen map[string]bool
}

// see writes the scratch space's own map.
func (current *scratch) see(key string) {
	current.seen[key] = true
}

// fresh is new scratch space, seen once.
func fresh(key string) *scratch {
	made := &scratch{seen: map[string]bool{}}
	made.see(key)
	return made
}

// names is every name registered, kept for the whole process.
var names = map[string]bool{}

// registered is the first name, registered as the package initializes.
var registered = register("first")

// register writes names, and only initializers call it, so it writes before any pass.
func register(name string) string {
	names[name] = true
	return name
}

// init registers a name as the package initializes, before any pass.
func init() {
	names["init"] = true
}

// announced is a name announced as the package initializes.
var announced = announce("announced")

// announce writes names, and ordinary code calls it too, so its write may be a pass's.
func announce(name string) string {
	names[name] = true // written: package state
	return name
}

// announceLater announces name whenever it is called.
func announceLater(name string) {
	announce(name)
}

// later is a function built as the package initializes, which writes whenever it is called.
var later = deferred()

// deferred builds later; it runs only as the package initializes, and the literal it returns runs after.
func deferred() func() {
	return func() {
		names["later"] = true // written: package state
	}
}

// limit is a flag value whose setter only flag calls, through the interface.
type limit int

// String is the limit as flag prints it.
func (current *limit) String() string {
	return "limit"
}

// Set takes the limit from the text flag hands it.
func (current *limit) Set(text string) error {
	*current = limit(len(text))
	return nil
}

// configuredLimit is the process's limit.
var configuredLimit limit

// ceiling is a flag value whose setter ordinary code calls by name.
type ceiling int

// String is the ceiling as flag prints it.
func (current *ceiling) String() string {
	return "ceiling"
}

// Set takes the ceiling from text.
func (current *ceiling) Set(text string) error {
	*current = ceiling(len(text)) // written: package state
	return nil
}

// configuredCeiling is the process's ceiling.
var configuredCeiling ceiling

// raise sets the ceiling by name, a call the guard sees.
func raise() error {
	return configuredCeiling.Set("raised")
}

// swept counts what the janitor swept, kept for the whole process.
var swept int

// init starts the janitor on a goroutine of its own, which runs whenever the scheduler runs it, a pass's time too.
func init() {
	go janitor()
}

// janitor sweeps, called only by the go statement init runs, which is no call made while the package initializes.
func janitor() {
	swept++ // written: package state
}

// queue is the items the process holds, in order.
type queue struct {
	items []int
}

// pending is the process's one queue.
var pending = &queue{items: make([]int, 4)}

// refill copies source over the queue's items, writing the queue's array.
func (held *queue) refill(source []int) {
	copy(held.items, source) // written: package state
}
