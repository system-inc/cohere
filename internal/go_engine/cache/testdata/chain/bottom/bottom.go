package bottom

import _ "embed"

//go:embed data.txt
var data string

// Bottom is the start of the chain.
func Bottom() string { return data }
