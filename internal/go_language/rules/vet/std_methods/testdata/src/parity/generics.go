package parity

import (
	"fmt"
	"io"
)

// box is generic, and its methods' types may be its type parameters.
type box[T any] struct{ value T }

func (box[T]) ReadByte() (T, error)          { var zero T; return zero, nil }
func (box[_]) Format(fmt.State, rune)        {}
func (*box[T]) WriteTo(io.Writer) (T, error) { var zero T; return zero, nil }
func (box[T]) GobDecode(T) error             { return nil }

// pair has two type parameters.
type pair[K comparable, V any] map[K]V

func (pair[K, V]) Seek(int64, int) (K, V) { var key K; var value V; return key, value }
func (pair[_, _]) UnreadRune() error      { return nil }

// genericError implements error, and its Unwrap returns its type parameter, which is not error by name.
type genericError[E error] struct{ inner E }

func (genericError[E]) Error() string { return "" }
func (genericError[E]) Unwrap() E     { return *new(E) }
func (genericError[E]) Is(E) bool     { return false }

// cleanGenericError's methods are canonical.
type cleanGenericError[T any] struct{ value T }

func (cleanGenericError[T]) Error() string { return "" }
func (cleanGenericError[T]) Unwrap() error { return nil }
func (*cleanGenericError[T]) As(any) bool  { return false }
