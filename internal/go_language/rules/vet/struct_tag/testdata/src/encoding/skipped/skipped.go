// Package skipped is under encoding/, whose packages know how to use their own tags, so nothing in it is reported.
package skipped

// Everything holds a finding of each kind the pass makes anywhere else.
type Everything struct {
	Malformed  int "hello"
	unexported int `json:"u"`
	First      int `json:"a"`
	Second     int `json:"a"`
	Spaced     int `json:"b, omitempty"`
}
