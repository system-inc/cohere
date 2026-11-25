package security_no_interpolated_sql_string

// template is a query read as texts with holes between them, one more text than holes, the shape package
// sql_text reads.
type template struct {
	texts []string
	holes []hole
}

// appendTemplate writes next after what template already holds, joining the text at the seam.
func (query *template) appendTemplate(next template) {
	query.texts[len(query.texts)-1] += next.texts[0]
	query.texts = append(query.texts, next.texts[1:]...)
	query.holes = append(query.holes, next.holes...)
}
