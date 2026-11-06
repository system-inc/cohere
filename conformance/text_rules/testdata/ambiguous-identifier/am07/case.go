package am07

type Event struct{ Target string }

func onClick(e Event) string {
	return e.Target
}
