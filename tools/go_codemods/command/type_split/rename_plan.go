package main

// renamePlan is what the name pass changes in one directory: new content per file name, nil to remove one.
type renamePlan struct {
	changes map[string][]byte
	moves   []fileMove
}

// rename moves file's content to name.
func (plan *renamePlan) rename(file *sourceFile, name string) {
	plan.changes[name] = file.source
	plan.changes[file.name] = nil
	plan.moves = append(plan.moves, fileMove{from: file.name, to: name, whole: true, continues: true})
}
