package micromark

// ConstructRecord maps a code to the constructs that may start there. Null holds upstream's `null` key,
// the constructs tried at every code.
type ConstructRecord struct {
	ByCode map[Code][]*Construct
	Null   []*Construct

	// withNull is each code's list followed by Null, made once when the record is combined, so an attempt
	// at a code builds no list (#vbjv3d6). Nil for a record made by hand.
	withNull map[Code][]*Construct
}

// constructsAt is upstream's handleMapOfConstructs list: the constructs for the code, then those for every
// code, and none at the end of input. Callers only read it.
func (record *ConstructRecord) constructsAt(code Code) []*Construct {
	if code == CodeEof {
		return nil
	}
	if record.withNull != nil {
		if list, found := record.withNull[code]; found {
			return list
		}
		return record.Null
	}
	var list []*Construct
	list = append(list, record.ByCode[code]...)
	return append(list, record.Null...)
}

// combine fills withNull from ByCode and Null.
func (record *ConstructRecord) combine() {
	record.withNull = make(map[Code][]*Construct, len(record.ByCode))
	for code, list := range record.ByCode {
		record.withNull[code] = append(append([]*Construct(nil), list...), record.Null...)
	}
}

func (*ConstructRecord) isConstructs() {}
