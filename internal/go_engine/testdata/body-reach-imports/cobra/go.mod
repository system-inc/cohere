module example.test/gate

// go 1.22: SA1015 reports time.Tick only before 1.23, which collects an unreferenced ticker.
go 1.22

require github.com/spf13/cobra v1.0.0

replace github.com/spf13/cobra => ./cobra
