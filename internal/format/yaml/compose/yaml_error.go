package compose

// YamlError is a YAMLParseError or a YAMLWarning.
type YamlError struct {
	// Name is "YAMLParseError" or "YAMLWarning".
	Name string
	// Code is upstream's ErrorCode, such as "BAD_INDENT" or "MISSING_CHAR".
	Code    string
	Message string
	// Position is the [start, end] of the error in UTF-16 code units.
	Position [2]int
}

func (yamlError *YamlError) Error() string { return yamlError.Message }
