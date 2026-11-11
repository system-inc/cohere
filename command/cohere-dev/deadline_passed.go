package main

import (
	"fmt"
	"time"
)

// deadlinePassed is the error of a command its bounded run's deadline stopped (bound.go).
type deadlinePassed struct {
	name     string
	deadline time.Duration
}

func (passed *deadlinePassed) Error() string {
	return fmt.Sprintf("%s passed the bounded run's deadline of %s, so its process group was stopped",
		passed.name, passed.deadline)
}
