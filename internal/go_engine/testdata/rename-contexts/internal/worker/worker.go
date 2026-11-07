// Package worker runs jobs under a caller's context.
package worker

import "context"

// Run runs one job, unless its caller's context is already done. Its context is the caller's, so it is named for
// that at every use.
func Run(ctx context.Context, job func(context.Context) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return job(ctx)
}

// Detach runs a job its caller can't cancel, in a context written twice, which a single rename can't split.
func Detach(parent context.Context, job func(context.Context) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	ctx = context.WithoutCancel(ctx)
	return job(ctx)
}
