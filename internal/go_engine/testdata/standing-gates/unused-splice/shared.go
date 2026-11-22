package gate

// compilerAgreementCase is built in Answer's body alone, and nothing reads its field group, so U1000's verdict on
// both hangs on that body (Adamic's shape): walked or spliced, group is reported; with the body simply absent, the
// type is, and group goes quiet under it.
type compilerAgreementCase struct {
	path, key, rule string
	group           int
}

// Answer stands in windows's build, which differs from the host's only in files Answer never reads.
func Answer() string {
	cases := []compilerAgreementCase{{path: "a", key: "b", rule: "all"}}
	return cases[0].path
}
