package gate

import _ "github.com/spf13/cobra"

// Other is windows's, whose file imports cobra, so SA1015 takes the package for a command's.
func Other() int { return 0 }
