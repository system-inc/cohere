// Package foreign stands in for a package we do not own, whose interfaces and types fix the names of the methods that
// satisfy them. The rule is not run over it: its abbreviated names are the ones the fixtures that import it must be
// allowed to repeat.
package foreign

// Source is a generic interface: a method of ours that implements one of its instances is named by it.
type Source[Item any] interface {
	Src() Item
	Close() error
}

// Target is a generic interface Copy takes only as a constraint.
type Target[Item any] interface {
	Dst() Item
}

// Copy reads a value out of anything that satisfies Target.
func Copy[Item any, Holder Target[Item]](holder Holder) Item {
	return holder.Dst()
}

// Sequence is a generic interface whose Seq names no type parameter, so its instance cannot be read off Seq alone.
type Sequence[Item any] interface {
	Push(item Item)
	Seq() int
}

// Pool is a generic concrete type whose method an interface of ours may be written to accept.
type Pool[Item any] struct {
	item Item
}

// Conn hands back the pooled item.
func (pool Pool[Item]) Conn() Item {
	return pool.item
}

// Commander is a plain interface whose method name the vocabulary abbreviates.
type Commander interface {
	Cmd() string
	Wait() error
}
