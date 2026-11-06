package settings_schema

import (
	"bytes"
	"encoding/json"
)

// additionalProperties is what an object holds beside the keys it names: false, refusing every other key,
// when schema is nil, or every other key with a value schema accepts.
type additionalProperties struct {
	schema *schemaNode
}

// noAdditionalProperties refuses every key an object does not name.
var noAdditionalProperties = &additionalProperties{}

// MarshalJSON writes false, or the schema. The schema is encoded without HTML escaping, as encode writes the
// rest, since the encoder around it keeps a value's escapes as it finds them.
func (additional additionalProperties) MarshalJSON() ([]byte, error) {
	if additional.schema == nil {
		return []byte("false"), nil
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encodeError := encoder.Encode(additional.schema); encodeError != nil {
		return nil, encodeError
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
