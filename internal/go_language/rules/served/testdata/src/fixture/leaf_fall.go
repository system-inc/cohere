package fixture

// Leaf is declared in leaf.go, so this file serves it only by holding a real method of it.
func (leaf *Leaf) Fall() {
	leaf.fallen = true
}
