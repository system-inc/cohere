// Package parity holds the Seek calls SA1013 judges, flagged and left alone, for the port and the original to judge
// side by side: every whence constant first, through interfaces, pointers, embedded and generic types, type
// parameters, renamed imports, parentheses and nested calls, and the calls it leaves alone.
package parity

import (
	"bytes"
	"io"
	seeking "io"
	"os"
	"strings"
)

type seeker struct{}

func (seeker) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type wrapper struct {
	seeker
}

type generic[Value any] struct{}

func (generic[Value]) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type swapped struct{}

func (swapped) Seek(whence int, offset int64) (int64, error) { return 0, nil }

type field struct {
	Seek func(offset int64, whence int) (int64, error)
}

type embedded struct {
	io.Seeker
}

type whence int

type namedWhence struct{}

func (namedWhence) Seek(offset int64, chosen whence) (int64, error) { return 0, nil }

type holder struct {
	SeekStart int64
}

func constrained[Seeking io.Seeker](seeker Seeking) {
	seeker.Seek(io.SeekEnd, 0)
}

func nested(interfaceSeeker io.Seeker) {
	interfaceSeeker.Seek(io.SeekStart, whenceOf(interfaceSeeker.Seek(io.SeekCurrent, 0)))
}

func whenceOf(offset int64, _ error) int { return int(offset) }

func flagged(interfaceSeeker io.Seeker, readSeeker io.ReadSeeker, file *os.File, reader *strings.Reader,
	byteReader *bytes.Reader, chosen int) {
	interfaceSeeker.Seek(io.SeekStart, 0)
	interfaceSeeker.Seek(io.SeekCurrent, chosen)
	interfaceSeeker.Seek(io.SeekEnd, -1)
	readSeeker.Seek(seeking.SeekStart, 0)
	file.Seek(io.SeekStart, 0)
	reader.Seek(io.SeekEnd, 0)
	byteReader.Seek(io.SeekStart, 0)
	interfaceSeeker.Seek((io.SeekStart), (chosen))
	(interfaceSeeker.Seek)(io.SeekStart, 0)
	seeker{}.Seek(io.SeekStart, 0)
	wrapper{}.Seek(io.SeekCurrent, 0)
	generic[int]{}.Seek(io.SeekEnd, 0)
	interfaceSeeker.Seek(io.SeekStart,
		chosen+1)
	_, _ = interfaceSeeker.Seek(io.SeekStart, 0)
	embedded{interfaceSeeker}.Seek(io.SeekStart, 0)
	(&embedded{}).Seek(seeking.SeekEnd, 0)
	go interfaceSeeker.Seek(io.SeekStart, 0)
	defer file.Seek(io.SeekCurrent, 0)
	func() { interfaceSeeker.Seek(io.SeekEnd, 0) }()
}

func spared(interfaceSeeker io.Seeker, file *os.File) {
	const SeekStart = 0
	interfaceSeeker.Seek(0, io.SeekStart)
	interfaceSeeker.Seek(SeekStart, 0)
	interfaceSeeker.Seek(0, 0)
	swapped{}.Seek(io.SeekStart, 0)
	field{}.Seek(io.SeekStart, 0)
	(*os.File).Seek(file, io.SeekStart, 0)
	seek := interfaceSeeker.Seek
	seek(io.SeekStart, 0)
	interfaceSeeker.Seek(io.SeekStart+0, 0)
	interfaceSeeker.Seek(int64(io.SeekStart), 0)
	namedWhence{}.Seek(io.SeekStart, 0)
	interfaceSeeker.Seek(holder{}.SeekStart, 0)
	io.NewSectionReader(nil, 0, 0).Seek(0, io.SeekStart)
}
