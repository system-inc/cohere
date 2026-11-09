package return_nil_ness

import "github.com/system-inc/cohere/internal/go_engine/intermediate_representation"

// calleeRecorder asks callees of each function a judgment calls, recording each of the package's own, which the push
// would have judged with it, so a replay can mark them seen without judging them again.
type calleeRecorder struct {
	callees   callees
	irPackage *intermediate_representation.Package
	recorded  []*intermediate_representation.Function
}

func (recorder *calleeRecorder) returns(function *intermediate_representation.Function) []ValueNilness {
	if function.Package == recorder.irPackage && function.Object() != nil {
		recorder.recorded = append(recorder.recorded, function)
	}
	return recorder.callees.returns(function)
}
