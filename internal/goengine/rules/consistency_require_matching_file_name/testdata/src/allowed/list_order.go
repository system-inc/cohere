package allowed

// Generic receivers spelled with different type parameter names are all List, and a theme follows its name.
func (list List[T]) Length() int {
	return len(list.items)
}

func (list *List[Element]) Last() Element {
	return list.items[len(list.items)-1]
}
