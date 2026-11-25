package gate

type Items struct{}

func (Items) Len() int           { return 0 }
func (Items) Less(int, int) bool { return false }
func (Items) Swap(int, int)      {}
