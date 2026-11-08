// Package parity holds the calls SA1012 judges, flagged and left alone, for the port and the original to judge side
// by side: a nil context passed to functions, methods, promoted and interface methods, generic functions and the
// standard library's, and the calls it leaves alone.
package parity

import "context"

type service struct{}

func (service) handle(ctx context.Context, name string) {}

func (*service) start(ctx context.Context) {}

func (*service) stop() {}

type outer struct {
	service
}

type handler interface {
	serve(ctx context.Context)
}

type box[Value any] struct{}

func (box[Value]) put(ctx context.Context, value Value) {}

func takes(ctx context.Context) {}

func takesSecond(name string, ctx context.Context) {}

func generic[Value any](ctx context.Context, value Value) {}

func nothing() {}

var hook func(ctx context.Context)

func flagged(value service, pointer *service, wrapped outer, served handler, boxed box[int]) {
	takes(nil)
	takes((nil))
	(takes)(nil)
	value.handle(nil, "")
	pointer.start(nil)
	wrapped.handle(nil, "")
	served.serve(nil)
	boxed.put(nil, 1)
	generic(nil, 1)
	generic[string](nil, "")
	(generic[int])(nil, 1)
	_, cancel := context.WithCancel(nil)
	cancel()
	_ = context.WithValue(nil, "key", "value")
}

func spared(ctx context.Context) {
	takes(ctx)
	takes(context.TODO())
	takesSecond("", nil)
	nothing()
	hook(nil)
	local := takes
	local(nil)
	_ = (func(context.Context))(nil)
	_ = error(nil)
	(*service).stop(nil)
	{
		nil := ctx
		takes(nil)
	}
}
