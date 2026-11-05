package allowed

import (
	"encoding/json"
	"net/http"
	stdURL "net/url"
	"strconv"
	_ "unsafe"

	"elsewhere"
)

// Names spelled as words, with an initialism capitalized at its first letter only.
const Id = 1

var userId = 2

type JsonEncoder struct{}

type HttpServer struct {
	UserId  string `json:"userID"`
	homeUrl string
}

func ParseUrl(requestId string) (responseUrl string) {
	parseUrlPath := requestId
	return parseUrlPath
}

// A single capital is no initialism, so neither a type parameter nor a word that opens a name is judged.
func Lookup[T comparable, K any](keys map[T]K) int {
	return len(keys)
}

var X, Y = 0.0, 0.0

// Every use of another package's name keeps its spelling: the package alias, the selector, the field read, and the
// constant.
func Fetch(request *http.Request) (string, error) {
	parsed, parseError := stdURL.Parse(request.URL.String())
	if parseError != nil {
		return "", parseError
	}
	encoded, marshalError := json.Marshal(parsed)
	if marshalError != nil {
		return "", marshalError
	}
	return strconv.Itoa(http.StatusOK) + string(encoded), nil
}

// Record's methods are named by json.Marshaler, json.Unmarshaler, fmt.Stringer, error and http.Handler, so a rename
// would stop them implementing those.
type Record struct{}

func (current Record) MarshalJSON() ([]byte, error) { return nil, nil }

func (current *Record) UnmarshalJSON(data []byte) error { return nil }

func (current Record) String() string { return "" }

func (current Record) Error() string { return "" }

func (current *Record) ServeHTTP(writer http.ResponseWriter, request *http.Request) {}

// account implements an interface of another package whose method spells an initialism, and that package owns it.
type account struct{}

func (current account) UserID() string { return "" }

var _ elsewhere.Identified = account{}

// office implements both of elsewhere.Located's methods, so both are its.
type office struct{}

func (current office) HomeURL() string { return "" }

func (current office) Region() string { return "" }

// keyed has the shape of elsewhere.Keyed's method, which some instantiation fixes.
type keyed struct{}

func (current keyed) KeyID() string { return "" }

var _ elsewhere.Keyed[string] = keyed{}

// handler is our interface, and its method mirrors http.Handler's exactly, so the interface implements http.Handler
// and the name is http's.
type handler interface {
	ServeHTTP(writer http.ResponseWriter, request *http.Request)
}

var _ handler = &Record{}

// The linker matches a //go:linkname's local name by name.
//
//go:linkname monotonicNS runtime.nanotime
func monotonicNS() int64

// cgo hands an //export function to C under its Go name.
//
//export GoCallbackID
func GoCallbackID() {}
