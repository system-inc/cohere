// Package kept has a path that begins with encoding but is not under encoding/, so it is judged as any other.
package kept

// Everything holds a finding of each kind the pass makes.
type Everything struct {
	Malformed  int "hello"
	unexported int `json:"u"`
	First      int `json:"a"`
	Second     int `json:"a"`
	Spaced     int `json:"b, omitempty"`
}
