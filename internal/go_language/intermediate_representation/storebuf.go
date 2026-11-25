package intermediate_representation

import "go/ast"

type storebuf struct{ stores []store }

func (storebuf *storebuf) store(lhs lvalue, rhs Value, source ast.Node) {
	storebuf.stores = append(storebuf.stores, store{lhs: lhs, rhs: rhs, source: source})
}

// storeDebugReference defers a debug reference with the stores, so it is emitted after the store it records.
func (storebuf *storebuf) storeDebugReference(reference *debugReference) {
	storebuf.stores = append(storebuf.stores, store{debugReference: reference})
}

func (storebuf *storebuf) emit(function *Function) {
	for _, store := range storebuf.stores {
		if store.debugReference != nil {
			function.emit(store.debugReference, nil)
			continue
		}
		store.lhs.store(function, store.rhs, store.source)
	}
}
