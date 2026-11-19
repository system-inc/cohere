package build_reads

import (
	"go/constant"
	"go/token"
	"math/big"
	"testing"
)

// A float constant reads alike whether go/constant holds it as a binary fraction, as export data gives the standard
// library's, or as a ratio, as a check from source does, so the body differential compares a body that reads math's
// constants on the host, which restores the standard library, with the same body in a build that checks it from
// source (#w72zxtq). A different number still reads apart.
func TestAFloatConstantReadsTheSameHeldEitherWay(test *testing.T) {
	test.Parallel()

	ratio := constant.MakeFromLiteral("340282346638528859811704183484516925440", token.FLOAT, 0)
	binary := constant.Make(new(big.Float).SetMantExp(big.NewFloat(0xffffff), 104))
	if exactConstant(ratio) != exactConstant(binary) {
		test.Errorf("one number reads %s held as a ratio and %s held as a binary fraction", exactConstant(ratio),
			exactConstant(binary))
	}
	other := constant.MakeFromLiteral("0.5", token.FLOAT, 0)
	if exactConstant(other) == exactConstant(ratio) {
		test.Errorf("two numbers both read %s", exactConstant(other))
	}
	complexValue := constant.BinaryOp(constant.MakeImag(binary), token.ADD, other)
	if written := exactConstant(complexValue); written != "(1/2 + 340282346638528859811704183484516925440i)" {
		test.Errorf("a complex constant reads %s", written)
	}
}
