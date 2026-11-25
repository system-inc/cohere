package platform

// Lock holds whether it is held, on windows.
type Lock struct{ held bool }

// Held reports whether the lock is held.
func (lock Lock) Held() bool { return lock.held }
