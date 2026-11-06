package changelog

// Bump is how much a version must move.
type Bump int

func (bump Bump) String() string {
	return [...]string{"patch", "minor", "major"}[bump]
}
