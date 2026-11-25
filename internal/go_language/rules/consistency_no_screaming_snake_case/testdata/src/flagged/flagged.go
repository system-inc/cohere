package flagged

import "testing"

// Package-level names keep their export in the suggestion.
const MAX_RETRIES = 3 // want "`MAX_RETRIES` is in screaming snake case; spell it `MaxRetries`"

const HTTP_CLIENT_TIMEOUT = 5 // want "`HTTP_CLIENT_TIMEOUT` is in screaming snake case; spell it `HttpClientTimeout`"

const default_PORT = 8080 // want "`default_PORT` has an underscore in it; spell it `defaultPort`"

var max_retries = 3 // want "`max_retries` has an underscore in it; spell it `maxRetries`"

var maxRetries_ = 3 // want "`maxRetries_` has an underscore in it; spell it `maxRetries`"

var _hidden = 1 // want "`_hidden` has an underscore in it; spell it `hidden`"

// A leading underscore makes a name unexported in Go, so its suggestion opens lowercase.
var _Hidden = 1 // want "`_Hidden` has an underscore in it; spell it `hidden`"

var MAX_ = 1 // want "`MAX_` has an underscore in it; spell it `Max`"

// A word in mixed case keeps its inner capitals: the initialism is another rule's business.
var userID_count = 1 // want "`userID_count` has an underscore in it; spell it `userIDCount`"

var HTTPClient_pool = 1 // want "`HTTPClient_pool` has an underscore in it; spell it `HTTPClientPool`"

func parse_URL() {} // want "`parse_URL` has an underscore in it; spell it `parseUrl`"

func Parse_line( // want "`Parse_line` has an underscore in it; spell it `ParseLine`"
	request_id string, // want "`request_id` has an underscore in it; spell it `requestId`"
) (read_count int) { // want "`read_count` has an underscore in it; spell it `readCount`"
	line_count := len(request_id) // want "`line_count` has an underscore in it; spell it `lineCount`"
	// A local is never exported, so even a shouting constant opens lowercase.
	const LOCAL_LIMIT = 4 // want "`LOCAL_LIMIT` is in screaming snake case; spell it `localLimit`"
	var Local_total int   // want "`Local_total` has an underscore in it; spell it `localTotal`"

	for row_index, cell_value := range []int{line_count} { // want "`row_index` has an" "`cell_value` has an"
		Local_total += row_index + cell_value + LOCAL_LIMIT
	}
	return Local_total
}

type Http_client struct { // want "`Http_client` has an underscore in it; spell it `HttpClient`"
	Base_url    string // want "`Base_url` has an underscore in it; spell it `BaseUrl`"
	retry_count int    // want "`retry_count` has an underscore in it; spell it `retryCount`"
}

func (my_client *Http_client) Send_request() int { // want "`my_client` has an" "`Send_request` has an"
	return my_client.retry_count
}

type Reader_closer interface { // want "`Reader_closer` has an underscore in it; spell it `ReaderCloser`"
	Read_all(byte_count int) error // want "`Read_all` has an underscore in it" "`byte_count` has an underscore"
}

type Handler func(request_body string) // want "`request_body` has an underscore in it; spell it `requestBody`"

// A type parameter keeps the case its author gave it.
func Map_keys[ // want "`Map_keys` has an underscore in it; spell it `MapKeys`"
	Key_type comparable, // want "`Key_type` has an underscore in it; spell it `KeyType`"
	value_type any, // want "`value_type` has an underscore in it; spell it `valueType`"
](table map[Key_type]value_type) []Key_type {
	var keys []Key_type
	for key := range table {
		keys = append(keys, key)
	}
	return keys
}

func describe(subject any) string {
	switch typed_value := subject.(type) { // want "`typed_value` has an underscore in it; spell it `typedValue`"
	case string:
		return typed_value
	}
	return ""
}

func countDown() {
outer_loop: // want "`outer_loop` has an underscore in it; spell it `outerLoop`"
	for {
		break outer_loop
	}
}

func localType() {
	type local_shape struct{} // want "`local_shape` has an underscore in it; spell it `localShape`"
	_ = local_shape{}
}

// A sentence that mentions //export mention_only is no directive, so the name stays ours.
func mention_only() {} // want "`mention_only` has an underscore in it; spell it `mentionOnly`"

// Outside a _test.go file an Example name is an ordinary function, and the underscore is ours.
func Example_helper() {} // want "`Example_helper` has an underscore in it; spell it `ExampleHelper`"

// Outside a _test.go file a test's name is no test's, so the whole name is ours.
func TestOutside_File(test *testing.T) {} // want "`TestOutside_File` has an underscore in it; spell it `TestOutsideFile`"
