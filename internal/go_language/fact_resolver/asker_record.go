package fact_resolver

import "sync"

// askerRecord is, by each answer, the bodies whose rules asked it (Resolver.RecordAskers), under lock.
type askerRecord struct {
	lock   sync.Mutex
	askers map[answerKey]map[Asker]bool
}
