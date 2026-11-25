package strace_events

// A Joiner turns a trace's lines into its finished calls, joining each call strace split around
// another process's: `openat(..., O_RDONLY <unfinished ...>` and, later on the same pid,
// `<... openat resumed>) = 3` are one openat, finished where it resumed. Its zero value is ready.
//
// It holds one unfinished call a pid at a time, as strace leaves one: a process is in one call. An
// unfinished call no resumption ever completes is never a call, since strace printed no result for
// it; a resumption with nothing unfinished on its pid is dropped, since its call's name and first
// arguments are lost.
type Joiner struct {
	started map[string]string
}

// Join is the call a line finishes, if it finishes one: a CallLine's own call, or a ResumedLine's
// call joined with the UnfinishedLine its pid left. Every other line finishes none. A consumer that
// places a call where it started, as Loom places a fork, reads the UnfinishedLine's Name itself
// before it joins.
func (joiner *Joiner) Join(line Line) (Call, bool) {
	switch line.Kind {
	case CallLine:
		return line.Call, true
	case UnfinishedLine:
		if joiner.started == nil {
			joiner.started = map[string]string{}
		}
		joiner.started[line.Pid] = line.Started
	case ResumedLine:
		started, found := joiner.started[line.Pid]
		if !found {
			return Call{}, false
		}
		delete(joiner.started, line.Pid)
		call, isCall := parseCall(started + line.Resumed)
		if !isCall {
			return Call{}, false
		}
		call.Pid = line.Pid
		return call, true
	}
	return Call{}, false
}
