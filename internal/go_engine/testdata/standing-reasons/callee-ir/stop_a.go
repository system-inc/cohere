//go:build !audit_b

package audit

func stop() { println("returns") }
