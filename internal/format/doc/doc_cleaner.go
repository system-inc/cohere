package doc

import "github.com/system-inc/cohere/internal/format/arena"

// docCleaner is CleanDoc's walk. It is MapDoc with cleanDocFn, with one difference: a concat's cleaned
// children are gathered in buffer, one stretch per concat on the walk's path, and the concat is built
// once, at its cleaned length, or handed back as it is when cleaning leaves it unchanged. MapDoc builds
// the mapped concat, then cleanDocFn builds the cleaned one by appending.
type docCleaner struct {
	// mapped is MapDoc's cache by identity, made at the first node that can carry one.
	mapped map[Doc]Doc
	buffer []Doc
	rec    func(Doc) Doc
	// slab is where the concats cleaning rebuilds come from, nil to allocate each (#v6ksqg3).
	slab *arena.Slab[Doc]
}

func (cleaner *docCleaner) clean(document Doc) Doc {
	cacheable := isPointerDoc(document)
	if cacheable {
		if result, present := cleaner.mapped[document]; present {
			return result
		}
	}
	var result Doc
	if parts, isArray := Parts(document); isArray {
		result = cleaner.cleanConcat(document, parts)
	} else {
		result = mapOne(document, cleaner.rec, cleanDoc)
	}
	if cacheable {
		if cleaner.mapped == nil {
			cleaner.mapped = map[Doc]Doc{}
		}
		cleaner.mapped[document] = result
	}
	return result
}

// cleanConcat is mapOne and cleanDocFn for a concat. Each child's own walk leaves buffer as it found it,
// so the concat's children are buffer[start:] once they are all cleaned.
func (cleaner *docCleaner) cleanConcat(document Doc, parts []Doc) Doc {
	start := len(cleaner.buffer)
	changed := false
	for _, part := range parts {
		cleaned := cleaner.clean(part)
		if !sameDoc(cleaned, part) {
			changed = true
		}
		cleaner.buffer = append(cleaner.buffer, cleaned)
	}
	children := cleaner.buffer[start:]
	var result Doc
	if !changed && isCleanConcat(parts) {
		result = document
	} else {
		result = cleanParts(children, cleaner.slab)
	}
	clear(children)
	cleaner.buffer = cleaner.buffer[:start]
	return result
}
