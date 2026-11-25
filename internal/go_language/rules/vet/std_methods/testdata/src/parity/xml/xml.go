// Package xml is not encoding/xml: it is a package of the same name whose types the parity fixture's MarshalXML and
// UnmarshalXML take, since both the original and the port compare types by the name of their package, not its path.
package xml

type Encoder struct{}

type Decoder struct{}

type StartElement struct{}
