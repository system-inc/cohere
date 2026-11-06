package main

import "strings"

// pathList collects a repeatable flag.
type pathList []string

func (list *pathList) String() string { return strings.Join(*list, ",") }

func (list *pathList) Set(value string) error {
	*list = append(*list, value)
	return nil
}
