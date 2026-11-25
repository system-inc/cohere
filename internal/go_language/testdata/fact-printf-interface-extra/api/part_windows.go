package api

// printer is a Logger in this build, so Logger.Logf wraps fmt.Printf here.
var _ Logger = printer{}
