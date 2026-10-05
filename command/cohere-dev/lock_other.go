//go:build !unix

package main

import "os"

// tryLockFile always takes the slot where flock does not exist: the slots are for the machines the house
// develops on, which are all Unix, and a run there is never held back by a lock it cannot take.
func tryLockFile(*os.File) (bool, error) {
	return true, nil
}

func unlockFile(*os.File) {}
