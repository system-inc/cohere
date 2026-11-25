// Package targeted_golden holds a call to shared in a file for 386 alone, and that file's golden file, the call
// replaced. Loaded for 386 the golden file is held, and loaded for any other target, as the machine's own, no file
// with a golden file is built.
package targeted_golden

func shared() {}

func kept() {}
