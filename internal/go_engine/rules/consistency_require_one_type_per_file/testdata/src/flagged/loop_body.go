package flagged

// The real method that makes Loop count in statement.go, though its marker method sits there. It has no result, so
// only its body tells it from a marker.
func (loop *Loop) Repeat(statement Statement) {
	loop.body.statements = append(loop.body.statements, statement)
}
