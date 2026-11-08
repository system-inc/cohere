// Package methods holds selectors of every kind, for the port of IsMethod and staticcheck's to judge side by side:
// method values of concrete, pointer, interface, embedded and generic types whose signatures match io.Seeker's Seek or
// do not, method expressions, fields, qualified identifiers and package-level functions.
package methods

import (
	"io"
	"os"
	"strings"
)

type seeker struct{}

func (seeker) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type swapped struct{}

func (swapped) Seek(whence int, offset int64) (int64, error) { return 0, nil }

type pointerSeeker struct{}

func (*pointerSeeker) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type renamed struct{}

func (renamed) Find(offset int64, whence int) (int64, error) { return 0, nil }

type embedding struct {
	seeker
	Seek2 func(offset int64, whence int) (int64, error)
}

type generic[Value any] struct{}

func (generic[Value]) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type field struct {
	Seek func(offset int64, whence int) (int64, error)
}

func selectors(file *os.File, reader *strings.Reader, interfaceSeeker io.Seeker, readSeeker io.ReadSeeker) {
	_, _ = seeker{}.Seek(0, io.SeekStart)
	_, _ = swapped{}.Seek(0, 0)
	_, _ = (&pointerSeeker{}).Seek(0, 0)
	_, _ = renamed{}.Find(0, 0)
	_, _ = embedding{}.Seek(0, 0)
	_, _ = embedding{}.Seek2(0, 0)
	_, _ = generic[int]{}.Seek(0, 0)
	_, _ = field{}.Seek(0, 0)
	_, _ = file.Seek(0, io.SeekCurrent)
	_, _ = reader.Seek(0, io.SeekEnd)
	_, _ = interfaceSeeker.Seek(0, 0)
	_, _ = readSeeker.Seek(0, 0)
	_, _ = seeker.Seek(seeker{}, 0, 0)
	_, _ = (*os.File).Seek(file, 0, 0)
	_, _ = io.ReadAll(reader)
	_ = strings.ToUpper("")
	_ = file.Close
}
