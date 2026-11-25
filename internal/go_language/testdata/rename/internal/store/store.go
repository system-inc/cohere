// Package store is the rename fixture's package of declarations: each one the naming rules flag is renamed at every
// reference or kept with a reason.
package store

import "encoding/json"

// maxLen is the longest a parsed text may be. Its name takes two renames in one run.
const maxLen = 64

// cfgName is read by a generated file, which the run can't write, so it keeps its name.
var cfgName = "store"

// groupID would become groupId, which is taken, so it keeps its name.
var groupID = "first"

var groupId = "second"

// ParseURL keeps text no longer than the limit. It is renamed here, in both test packages and in the command.
func ParseURL(text string) string {
	if len(text) > maxLen {
		return text[:maxLen]
	}
	return text
}

// Fetch fetches through the interface, so the method is renamed with its implementation.
func Fetch() string {
	return ParseURL(Fetcher(page{}).FetchURL()) + generatedName + groupID + groupId + readHTML()
}

// Record is encoded by its field names, so OwnerID keeps its name.
type Record struct {
	OwnerID string
}

// Encode encodes a record.
func Encode(record Record) ([]byte, error) {
	return json.Marshal(record)
}

// readHTML keeps its name, by a directive.
//
//cohere:ignore cohere-go/consistency-no-uppercase-initialism the name is the fixture's directive case
func readHTML() string {
	return cfgName
}
