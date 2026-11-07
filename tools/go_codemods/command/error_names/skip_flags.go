package main

import "strings"

// skipFlags collects repeated -skip values.
type skipFlags []string

func (skips *skipFlags) String() string { return strings.Join(*skips, ",") }

func (skips *skipFlags) Set(value string) error {
	*skips = append(*skips, value)
	return nil
}
