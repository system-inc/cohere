package gate

import _ "github.com/jessevdk/go-flags"

// Other is windows's, whose file imports go-flags, so SA5008 accepts a repeated default key.
func Other() int { return 0 }
