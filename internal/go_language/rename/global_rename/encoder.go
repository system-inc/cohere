package global_rename

// encoder is a set of encoding families: what reads a struct's field names at run time.
type encoder uint16

func (set encoder) names() []string {
	var names []string
	for _, entry := range encoderNames {
		if set&entry.encoder != 0 {
			names = append(names, entry.name)
		}
	}
	return names
}
