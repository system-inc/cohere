package printing

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// attachment is one comment's placement in the differential's summary: where the comment starts, whether
// it leads, trails or dangles, and the type and start of the node it is on. It travels as upstream's
// `[comment.start, kind, node.type, node.start]` tuple, so both sides' summaries compare as JSON text.
type attachment struct {
	CommentStart int
	Kind         string
	NodeType     string
	NodeStart    int
}

// MarshalJSON writes the tuple upstream's script pushes.
func (entry attachment) MarshalJSON() ([]byte, error) {
	kind, kindError := json.Marshal(entry.Kind)
	if kindError != nil {
		return nil, fmt.Errorf("encoding the attachment kind: %w", kindError)
	}
	nodeType, nodeTypeError := json.Marshal(entry.NodeType)
	if nodeTypeError != nil {
		return nil, fmt.Errorf("encoding the attachment node type: %w", nodeTypeError)
	}
	encoded := []byte{'['}
	encoded = strconv.AppendInt(encoded, int64(entry.CommentStart), 10)
	encoded = append(encoded, ',')
	encoded = append(encoded, kind...)
	encoded = append(encoded, ',')
	encoded = append(encoded, nodeType...)
	encoded = append(encoded, ',')
	encoded = strconv.AppendInt(encoded, int64(entry.NodeStart), 10)
	return append(encoded, ']'), nil
}

// UnmarshalJSON reads the tuple upstream's script writes.
func (entry *attachment) UnmarshalJSON(data []byte) error {
	var fields []json.RawMessage
	if decodeError := json.Unmarshal(data, &fields); decodeError != nil {
		return fmt.Errorf("decoding an attachment tuple: %w", decodeError)
	}
	if len(fields) != 4 {
		return fmt.Errorf("decoding an attachment tuple: %d fields, not 4", len(fields))
	}
	if decodeError := json.Unmarshal(fields[0], &entry.CommentStart); decodeError != nil {
		return fmt.Errorf("decoding the attachment's comment start: %w", decodeError)
	}
	if decodeError := json.Unmarshal(fields[1], &entry.Kind); decodeError != nil {
		return fmt.Errorf("decoding the attachment kind: %w", decodeError)
	}
	if decodeError := json.Unmarshal(fields[2], &entry.NodeType); decodeError != nil {
		return fmt.Errorf("decoding the attachment node type: %w", decodeError)
	}
	if decodeError := json.Unmarshal(fields[3], &entry.NodeStart); decodeError != nil {
		return fmt.Errorf("decoding the attachment's node start: %w", decodeError)
	}
	return nil
}
