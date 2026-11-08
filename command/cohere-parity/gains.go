package main

import (
	"fmt"
	"os"
	"strings"
)

// gainCommentPrefix opens a reason line in a gains file.
const gainCommentPrefix = "#"

// gain is one finding a port makes over a whole tree that its original does not, recorded with why: finding is the
// line exactly as the census prints it, the path relative to the tree's root, and reason is the comment above it.
type gain struct {
	finding string
	reason  string
}

// readGains reads a gains file: each finding on a line of its own, the reason on the comment lines, opening with
// gainCommentPrefix, directly above it. A blank line ends a reason, so every gain says its own, and a gain with none
// is refused, since an unexplained gain is what the file exists to rule out.
func readGains(path string) ([]gain, error) {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return nil, fmt.Errorf("reading the gains file: %w", readError)
	}
	gains := []gain{}
	reason := []string{}
	for index, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			reason = reason[:0]
		case strings.HasPrefix(line, gainCommentPrefix):
			reason = append(reason, strings.TrimSpace(strings.TrimPrefix(line, gainCommentPrefix)))
		case len(reason) == 0:
			return nil, fmt.Errorf("%s:%d records a gain with no reason above it: %s", path, index+1, line)
		default:
			gains = append(gains, gain{finding: line, reason: strings.Join(reason, " ")})
			reason = reason[:0]
		}
	}
	return gains, nil
}
