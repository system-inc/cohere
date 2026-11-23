// Package records imports nothing of ours, reading the same std types as the diamond does at the same time.
package records

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Record is one timed entry.
type Record struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Encode writes records as json, one a line.
func Encode(writer io.Writer, records []Record) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	for _, record := range records {
		if encodeError := encoder.Encode(record); encodeError != nil {
			return fmt.Errorf("encoding %q: %w", strings.TrimSpace(record.Text), encodeError)
		}
	}
	_, writeError := io.Copy(writer, &buffer)
	return writeError
}
