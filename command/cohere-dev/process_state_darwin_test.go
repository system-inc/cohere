package main

import "golang.org/x/sys/unix"

// zombieState is a process's p_stat once it has exited and before its parent reaps it, `SZOMB` in xnu's sys/proc.h,
// which x/sys names no constant for.
const zombieState = 5

// isZombie reports whether pid has exited and waits only to be reaped, read from the kernel by pid.
func isZombie(pid int) bool {
	process, sysctlError := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return sysctlError == nil && process.Proc.P_pid == int32(pid) && process.Proc.P_stat == zombieState
}
