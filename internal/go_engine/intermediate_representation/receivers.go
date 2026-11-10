// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines what a method's receiver gives: its type, its type arguments, and the method's signature
// with the receiver as its first parameter.

import "go/types"

// recvType returns the receiver type of method obj.
func recvType(object *types.Func) types.Type {
	sig, isSignature := object.Type().(*types.Signature)
	if !isSignature {
		// Invariant: go/types gives every *types.Func a *types.Signature type.
		panic("not a function: " + object.String())
	}
	return sig.Recv().Type()
}

// receiverTypeArguments returns the type arguments to a method's receiver.
// Returns an empty list if the receiver does not have type arguments.
func receiverTypeArguments(method *types.Func) []types.Type {
	recv := method.Signature().Recv()
	named := func() (named *types.Named) {
		resolvedType := recv.Type()
		if ptr, ok := types.Unalias(resolvedType).(*types.Pointer); ok {
			resolvedType = ptr.Elem()
		}
		named, _ = types.Unalias(resolvedType).(*types.Named)
		return named
	}()
	if named == nil {
		return nil // recv is anonymous struct/interface
	}
	ts := named.TypeArgs()
	if ts.Len() == 0 {
		return nil
	}
	targs := make([]types.Type, ts.Len())
	for index := 0; index < ts.Len(); index++ {
		targs[index] = ts.At(index)
	}
	return targs
}

// recvAsFirstArgument takes a method signature and returns a function
// signature with receiver as the first parameter.
func recvAsFirstArgument(sig *types.Signature) *types.Signature {
	parameters := make([]*types.Var, 0, 1+sig.Params().Len())
	parameters = append(parameters, sig.Recv())
	for variable := range sig.Params().Variables() {
		parameters = append(parameters, variable)
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), sig.Results(), sig.Variadic())
}
