package gate

// Record's Payload is noany's key, declared in the file both builds compile.
type Record struct{ Payload any }

// Build builds a Record, so it can mark Payload.
func Build() Record { return Record{} }

// Port touches no key.
func Port() int { return 80 }
