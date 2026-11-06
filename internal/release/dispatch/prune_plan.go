package dispatch

// PrunePlan is what a prune would do, decided without touching anything.
type PrunePlan struct {
	// Examined is how many entries the binary directory held. A plan that examined nothing says so,
	// so a prune pointed at the wrong directory cannot read as a tidy cache.
	Examined int

	// Remove are the files to delete, each with its size.
	Remove []PrunedFile

	// Unrecognised are names the prune kept because it could not classify them.
	Unrecognised []string

	// StaleCompilers are compiler extractions to remove: not the current build's, and unused for the
	// in-flight window. Bytes is the extraction's total size.
	StaleCompilers []PrunedFile

	// StaleSnapshots are snapshot directories a killed build left behind, older than the window.
	StaleSnapshots []PrunedFile
}

// Bytes is the total a plan reclaims.
func (plan PrunePlan) Bytes() int64 {
	total := int64(0)
	for _, list := range [][]PrunedFile{plan.Remove, plan.StaleCompilers, plan.StaleSnapshots} {
		for _, file := range list {
			total += file.Bytes
		}
	}
	return total
}
