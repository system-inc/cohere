package importing

type options struct {
	A int   `choice:"foo" choice:"bar"`
	B []int `optional-value:"foo" optional-value:"bar"`
	C []int `default:"foo" default:"bar"`
	D int   `json:"foo" json:"bar"`
	E int   `long:"foo" long:"bar"`
	F int   `choice:"foo" choice:"bar" default:"x" default:"y" xml:"a" xml:"b"`
}
