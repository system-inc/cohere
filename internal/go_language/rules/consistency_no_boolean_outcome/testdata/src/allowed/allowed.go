package allowed

import "errors"

// The repair: a named outcome with a constant for each way the lookup can turn out, the payload beside it.
type LookupOutcome string

const (
	LookupFound    LookupOutcome = "Found"
	LookupNotFound LookupOutcome = "NotFound"
)

type Lookup struct {
	Outcome LookupOutcome
	Value   string
}

// Go's own idiom for the two-way case: the value and an error, or the value and whether it was there.
func find(key string) (string, error) {
	if key == "" {
		return "", errors.New("finding a value for an empty key")
	}
	return key, nil
}

func cached(values map[string]int, key string) (value int, ok bool) {
	value, ok = values[key]
	return value, ok
}

// A flag on its own is state, not a result.
type Toggle struct {
	Ok bool
}

type Job struct {
	Name      string
	Succeeded bool
}

// TanStack Query's status field beside the flag mirrors a third party's result, and so does an HTTP reply's.
type Reply struct {
	Ok     bool
	Status int
	Error  error
}

type pending struct {
	isLoading bool
	isError   bool
	data      []byte
}

// Every field of Cloudflare API v4's envelope mirrors its wire format.
type CloudflareEnvelope struct {
	Success  bool     `json:"success"`
	Errors   []string `json:"errors"`
	Messages []string `json:"messages"`
	Result   any      `json:"result"`
}

// isSuccess beside isError is one settled state seen from two sides.
type Settled struct {
	IsSuccess bool
	IsError   bool
	Data      []byte
}

// A name ending Properties is display state, as the twins exempt it.
type BannerProperties struct {
	IsError bool
	Message string
}

func use() {
	_, _ = find("")
	_, _ = cached(nil, "")
}
