// Package plain imports no go-flags, so every repeated key is told.
package plain

type options struct {
	A int   `choice:"foo" choice:"bar"`
	B []int `optional-value:"foo" optional-value:"bar"`
	C []int `default:"foo" default:"bar"`
}
