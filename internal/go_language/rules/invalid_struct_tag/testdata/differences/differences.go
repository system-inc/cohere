// Package differences holds the tags where the port's reading could part from the original's: several keys in one
// tag, read in a map's order, a field list's position against a field's index, and a nested struct's fields. Both find
// the same here, so the fixture records no difference.
package differences

type shapes struct {
	A    int `json:",foreign" xml:",unknown" foo:"" foo:""`
	B, C int `xml:"a>"`
	D    int `xml:",attr,innerxml" json:",string,omitEmpty"`
	E    struct {
		F int `json:",foreign"`
	}
}
