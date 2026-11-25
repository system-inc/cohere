package parity

import (
	"testing"

	"other"
)

// Names go test runs and names it does not.

func TestGood(t *testing.T) {}

func Testbad(t *testing.T) {}

func Test(t *testing.T) {}

func Test_underscore(t *testing.T) {}

func Test1(t *testing.T) {}

func TestÉcole(t *testing.T) {}

func Testécole(t *testing.T) {}

func Testing(t *testing.T) {}

func Benchmark(b *testing.B) {}

func BenchmarkGood(b *testing.B) {}

func Benchmarkbad(b *testing.B) {}

func Fuzz(f *testing.F) {}

func FuzzGood(f *testing.F) {}

func Fuzzbad(f *testing.F) {}

// Shapes that are not go test's, which are not judged.

func Testresults(t *testing.T) error { return nil }

func Testnone() {}

func Testtwo(t *testing.T, name string) {}

func Testnames(first, second *testing.T) {}

func Testvalue(t testing.T) {}

func Testparenthesized(t *(testing.T)) {}

func Testbenchmark(b *testing.B) {}

func Benchmarktest(t *testing.T) {}

func Fuzztest(t *testing.T) {}

func TestMain(m *testing.M) {}

func Testmain(m *testing.M) {}

type suite struct{}

func (suite) Testmethod(t *testing.T) {}

// Only the name of the pointed type is read, so another package's T and a local B look like testing's.

func Testother(t *other.T) {}

type B struct{}

func Benchmarklocal(b *B) {}

// Type parameters, with a malformed name and without, over one parameter, two, and lines of their own.

func TestGeneric[T any](t *testing.T) {}

func Testgeneric[T any](t *testing.T) {}

func BenchmarkGeneric[T any, U comparable](b *testing.B) {}

func FuzzGeneric[T any](f *testing.F) {}

func TestSpread[
	T any,
	U any,
](t *testing.T) {
}

func Testresultsgeneric[T any](t *testing.T) T { var zero T; return zero }
