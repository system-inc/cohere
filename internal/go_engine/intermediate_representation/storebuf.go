package intermediate_representation

import "go/ast"

type storebuf struct{ stores []store }

func (storebuf *storebuf) store(lhs lvalue, rhs Value, source ast.Node) {
	storebuf.stores = append(storebuf.stores, store{lhs, rhs, source})
}

func (storebuf *storebuf) emit(function *Function) {
	for _, store := range storebuf.stores {
		store.lhs.store(function, store.rhs, store.source)
	}
}
