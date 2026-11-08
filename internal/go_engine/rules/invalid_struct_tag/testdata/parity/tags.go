package parity

import "encoding/xml"

type parsing struct {
	A int `foo:"" foo:""`
	B int `foo:"" bar:"" foo:"x"`
	C int `json:"a" json:"b"`
	D int `xml:"a" xml:"b"`
	E int `json:"\q"`
	F int `json:x`
	G int `json:"abc`
	H int `json :"a"`
	I int `  json:"a,foreign"  `
	J int `choice:"foo" choice:"bar"`
	K int `optional-value:"foo" optional-value:"bar"`
	L int `default:"foo" default:"bar"`
	M int `json:"a,foreign"xml:",unknown"`
	N int `json:"\u00e9,foreign"`
	O int `json:"a" bad`
	P int `:"a"`
	Q int ``
	R int `json:"\x"`
}

type interpreted struct {
	A int "json:\"a,foreign\""
	B int "foo:\"\" foo:\"\""
	C int "json:\",foreign\" xml:\",unknown\""
	D int "x:\"\\q\""
}

type xmlOptions struct {
	A int      `xml:",attr,attr"`
	B int      `xml:"name,chardata,cdata"`
	C int      `xml:",any,omitempty"`
	D int      `xml:"a,,"`
	E int      `xml:",Attr"`
	F int      `xml:",omitempty,omitempty,foo,foo"`
	G int      `xml:"a>b,attr"`
	H int      `xml:"ns name,attr"`
	I int      `xml:"-"`
	J int      `xml:"-,attr"`
	K int      `xml:",comment"`
	L xml.Name `xml:"other"`
	M int      `xml:"a>>b"`
	N int      `xml:">a"`
}

type xmlNamed struct {
	XMLName xml.Name `xml:"named"`
}

type xmlConflicts struct {
	A xmlNamed `xml:"other"`
	B xmlNamed `xml:"named"`
	C xmlNamed
}

type severalNames struct {
	A, B int `xml:"a>"`
	C    int `xml:"c"`
	D    int `xml:"d"`
}

type severalNamesMissed struct {
	A, B int `json:"a"`
	C    int `xml:"a>"`
}

type embeddedNames struct {
	xmlNamed `xml:"embedded,attr"`
	A, B     int `xml:",innerxml"`
	C        int `xml:",attr,innerxml"`
}

var variable struct {
	A int `json:",foreign"`
	B struct {
		C int `json:",foreign"`
	} `json:",foreign"`
}

func local() {
	type inside struct {
		A int `xml:",unknown"`
	}
	_ = struct {
		B int `json:",foreign"`
	}{}
	_ = func(parameter struct {
		C int `json:",foreign"`
	}) {
	}
}

type generic[T any] struct {
	A T             `json:",foreign"`
	B []T           `json:",string"`
	C struct{ D T } `xml:",unknown"`
}
