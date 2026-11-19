package fact_resolver

import "go/types"

// RecordAskers has the resolver record, from now on, the bodies whose rules ask each answer (NoteAsker), for the body
// differential (#5vbs1np, must 5). It is called before the run's passes start, and never in a run that does not
// measure the facts' reach, whose questions record nothing.
func (resolver *Resolver) RecordAskers() {
	resolver.askers = &askerRecord{askers: map[answerKey]map[Asker]bool{}}
}

// RecordsAskers is whether the resolver records askers. A Result asked For a function hands itself back when it does
// not, so a run that records nothing makes nothing for it.
func (resolver *Resolver) RecordsAskers() bool {
	return resolver.askers != nil
}

// NoteAsker records asker as a body whose rule asked kind's answer about object, when the resolver records askers and
// asker is a body. A run that records none takes no lock and allocates nothing here.
func (resolver *Resolver) NoteAsker(kind *Kind, object types.Object, asker *Asker) {
	record := resolver.askers
	if record == nil || asker == nil {
		return
	}
	record.lock.Lock()
	defer record.lock.Unlock()
	key := answerKey{kind: kind, object: object}
	if record.askers[key] == nil {
		record.askers[key] = map[Asker]bool{}
	}
	record.askers[key][*asker] = true
}

// Askers are the bodies whose rules asked each answer, by kind's name and object, each answer's in no order, read once
// the run's passes are done: nil when the resolver records none.
func (resolver *Resolver) Askers() map[string]map[types.Object][]Asker {
	record := resolver.askers
	if record == nil {
		return nil
	}
	record.lock.Lock()
	defer record.lock.Unlock()
	askers := map[string]map[types.Object][]Asker{}
	for key, bodies := range record.askers {
		if askers[key.kind.Name] == nil {
			askers[key.kind.Name] = map[types.Object][]Asker{}
		}
		for body := range bodies {
			askers[key.kind.Name][key.object] = append(askers[key.kind.Name][key.object], body)
		}
	}
	return askers
}
