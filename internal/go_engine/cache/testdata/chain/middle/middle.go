package middle

import "example.com/chain/bottom"

// Middle is the link between.
func Middle() string { return "middle " + bottom.Bottom() }
