// Package parity holds every way the original accepts or refuses a struct tag: tags that parse and tags that do not,
// keys given twice, every branch of the `json` grammar, names, options and the values of case and format, plain,
// single-quoted and malformed, the `string` option over every kind of type, type parameters among them, `xml` values
// and options, fields declared several to a line, tags written as interpreted strings, embedded and unexported fields,
// struct types nested, inside functions, in variables and in a generated file.
package parity

type jsonNames struct {
	A int `json:""`
	B int `json:"name"`
	C int `json:"'quoted',omitempty"`
	D int `json:"'unterminated"`
	E int `json:"bad\\name"`
	F int `json:"na'me"`
	G int `json:"\xff"`
	H int `json:"-,"`
	I int `json:"'-'"`
	J int `json:"-"`
	K int `json:"-,omitempty,omitempty"`
	L int `json:"a\"b"`
	M int `json:"名前"`
	N int `json:"some;thing"`
	O int `json:"'quo\"ted'"`
	P int `json:"'\\x'"`
	Q int `json:"'abcdefghijklmnop"`
	R int `json:"a b"`
	S int `json:"'a\\'b'"`
}

type jsonOptions struct {
	A int `json:"name,"`
	B int `json:"name,,omitempty"`
	C int `json:"name,omitEmpty"`
	D int `json:"name,omit_zero"`
	E int `json:"name,'omitempty'"`
	F int `json:"name,'omit empty'"`
	G int `json:"name,INLINE"`
	H int `json:",inline,unknown"`
	I int `json:",inline,embed"`
	J int `json:",unknown,unknown"`
	K int `json:",é"`
	L int `json:",'a\"b'"`
	M int `json:",'\\x'"`
	N int `json:",'abcdefghijklmnop"`
	O int `json:",omitzero;omitempty"`
	P int `json:"name;omitempty"`
	Q int `json:",9"`
	R int `json:",Case:ignore"`
	S int `json:",String"`
	T int `json:",For_Mat"`
}

type jsonValues struct {
	A int `json:"name,case"`
	B int `json:"name,case:ignore"`
	C int `json:"name,case:strict"`
	D int `json:"name,case:'ignore'"`
	E int `json:"name,case:other"`
	F int `json:"name,case:"`
	G int `json:"name,case:'unterminated"`
	H int `json:"name,case:ignore,case:strict"`
	I int `json:"name,format:RFC3339,omitempty"`
	J int `json:"name,format"`
	K int `json:"name,format:"`
	L int `json:"name,format:'with,commas'"`
	M int `json:"name,format:a,format:b"`
	N int `json:"name,format;x"`
	O int `json:"name,case;x"`
	P int `json:"name,format:'unterminated,omitempty"`
	Q int `json:"name,case:ignore;x"`
}

type named int

type namedString string

type alias = int

type namedPointer *int

type jsonStrings struct {
	A bool                   `json:",string"`
	B string                 `json:",string"`
	C float64                `json:",string"`
	D uint8                  `json:",string"`
	E *int                   `json:",string"`
	F **int                  `json:",string"`
	G []int                  `json:",string"`
	H map[string]int         `json:",string"`
	I interface{}            `json:",string"`
	J named                  `json:",string"`
	K namedString            `json:",string"`
	L alias                  `json:",string"`
	M *named                 `json:",string"`
	N struct{}               `json:",string"`
	O namedPointer           `json:",string"`
	P *namedPointer          `json:",string"`
	Q complex64              `json:",string"`
	R uintptr                `json:",string"`
	S chan int               `json:",string"`
	T func()                 `json:",string"`
	U [2]int                 `json:",string"`
	V int                    `json:",string,string"`
	W *string                `json:",string"`
	X interface{ M() }       `json:",string"`
	Y error                  `json:",string"`
	Z map[string]interface{} `json:",string,omitempty"`
}

type anyStrings[T any, C comparable, N interface{ ~int | ~float64 }, M interface{ ~int | complex64 }] struct {
	A T  `json:",string"`
	B T  `json:",string,string"`
	C C  `json:",string"`
	D N  `json:",string"`
	E M  `json:",string"`
	F *N `json:",string"`
	G *M `json:",string"`
}

type unexported int

type Exported int

type jsonFields struct {
	lower      int `json:"name"`
	lowerDash  int `json:"-"`
	lowerEmpty int `json:""`
	lowerComma int `json:",omitempty"`
	unexported `json:",foreign"`
	*Exported  `json:"exported,foreign"`
	_          int `json:"blank"`
	A, B       int `json:",foreign"`
	c, D       int `json:",omitempty"`
}
