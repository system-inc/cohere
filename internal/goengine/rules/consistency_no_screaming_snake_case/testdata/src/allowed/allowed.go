package allowed

// MixedCaps names, exported and not.
const MaxRetries = 3

const defaultPort = 8080

var HttpClientTimeout = 5

type HttpClient struct {
	BaseUrl    string
	retryCount int
}

func (client *HttpClient) SendRequest(requestId string) (readCount int) {
	lineCount := len(requestId)
	for rowIndex, cellValue := range []int{lineCount} {
		readCount += rowIndex + cellValue + client.retryCount
	}
	return readCount
}

// The blank identifier is Go's grammar for no name.
var _ = MaxRetries

func discard(values []int) {
	for _, value := range values {
		_ = value
	}
	_, _ = defaultPort, HttpClientTimeout
}

func MapKeys[KeyType comparable, Value any](table map[KeyType]Value) []KeyType {
	var keys []KeyType
	for key := range table {
		keys = append(keys, key)
	}
	return keys
}

// A single all-caps word has no underscore, so it is not this rule's shape.
const RED = "red"
