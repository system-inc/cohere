package printing

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// attachment is one comment's placement in TestCoreAgreesWithUpstream's summary: where the comment starts, whether
// it leads, trails or dangles, and the type and start of the node it is on. It travels as upstream's
// `[comment.start, kind, node.type, node.start]` tuple, so Go's summary, as JSON text, is what upstream's
// recorded fingerprint was taken of.
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
