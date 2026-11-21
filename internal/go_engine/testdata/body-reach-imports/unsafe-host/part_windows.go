package gate

// Other is windows's, whose file imports nothing here, the unsafe module turned round: the host's twin imports the
// path the gate reads, so unusedwrite reports Target in windows's build alone.
func Other() int { return 0 }
