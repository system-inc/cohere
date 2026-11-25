package parity

// FileLeaf is embedded in parity.go, so the message names the other file of a repeat by its name alone.
type FileLeaf struct {
	Name string `json:"name"`
}
