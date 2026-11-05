package flagged

// The plain case: a success flag beside the error that says what went wrong when it is false. The suggestion strips
// the role suffix.
type LookupResult struct {
	Success bool // want "Success bool. on struct LookupResult collapses .* Suggested name: LookupOutcome\\."
	Error   error
}

// Unexported fields are spelled as the twins' lists spell them.
type fetchReply struct {
	ok    bool // want "ok bool. on struct fetchReply"
	value string
}

// A failed flag beside a reason, the shape of a skip that might have been a failure.
type ProbeResponse struct {
	FileName string
	Reason   string
	Failed   bool // want "Failed bool. on struct ProbeResponse .* Suggested name: ProbeOutcome\\."
}

// Each remaining flag name, each beside a different companion.
type Upload struct {
	Succeeded bool // want "Succeeded bool"
	Errors    []error
}

type Attempt struct {
	IsSuccess bool // want "IsSuccess bool"
	Result    int
}

type Parse struct {
	IsOk bool // want "IsOk bool"
	Data []byte
}

type Check struct {
	IsError bool // want "IsError bool"
	Message string
}

type Sync struct {
	IsFailure bool // want "IsFailure bool"
	Value     float64
}

// Names grouped on one line are fields each: the flag is reported, wherever it sits in the group.
type Batch struct {
	Ready, Ok bool // want "Ok bool. on struct Batch"
	Data      []string
}

// Two flags make one finding, on the first in declaration order.
type Twice struct {
	Ok     bool // want "Ok bool. on struct Twice"
	Failed bool
	Error  error
}

// An alias of a struct literal is the same named field set, as a type alias of an object literal is in TypeScript.
type AliasReply = struct {
	Ok    bool // want "Ok bool. on struct AliasReply"
	Value int
}

// A generic envelope is still one bit for every outcome.
type Answer[Payload any] struct {
	Ok    bool // want "Ok bool. on struct Answer "
	Value Payload
}

// An embedded error is a field named error, reached as reply.error.
type embeddedReply struct {
	ok bool // want "ok bool. on struct embeddedReply"
	error
}

// An alias of bool is the predeclared bool, under another name.
type flagAlias = bool

type aliasedFlag struct {
	ok     flagAlias // want "ok bool. on struct aliasedFlag"
	reason string
}

// Two of Cloudflare's four envelope fields are not its envelope, so a shape of our own sharing them is read.
type PartialCloudflare struct {
	Success bool // want "Success bool. on struct PartialCloudflare"
	Errors  []string
	Result  any
}

// A struct declared in a function is a declaration too, as an interface declared in one is in TypeScript.
func local() any {
	type stepResult struct {
		failed bool // want "failed bool. on struct stepResult .* Suggested name: stepOutcome\\."
		reason string
	}
	return stepResult{}
}

// A multi-byte field name beside the flag leaves the span where the flag is.
type Größe struct {
	Maß     int
	Success bool // want "Success bool. on struct Größe"
	Message string
}

// A name that is nothing but role suffixes keeps itself as the suggestion.
type Result struct {
	Ok    bool // want "Suggested name: ResultOutcome\\."
	Value int
}
