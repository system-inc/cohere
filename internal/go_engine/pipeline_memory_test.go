package go_engine

import "testing"

func TestArmLateRaiseWithoutMemory(test *testing.T) {
	test.Parallel()
	engine := &pipeline{}

	raise := engine.armLateRaise()
	raise.endLoad()

	if raise != nil {
		test.Error("a run without a memory decision armed a late raise")
	}
}
