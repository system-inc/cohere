package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// queueTicket is a waiting run's place in line for a test slot (#nf1qj58).
//
// Waiters used to poll the slot's lock once a second, and whichever polled first after a release took it,
// so there was no order: at 04:26 on 2026-10-05 five members' runs were waiting, one of them for twenty
// minutes behind runs that arrived after it. Now each waiter holds a ticket, a file in the queue directory
// named so the names sort by arrival, and only the oldest live tickets may take a slot. The ticket is held
// under a lock, which the kernel drops however its run ends, so a ticket whose lock can be taken belongs to
// a waiter that is gone and is removed rather than waited on.
type queueTicket struct {
	file *os.File
	path string
}

// joinQueue takes a ticket at the back of the line.
//
// The ticket is locked under a name of its own first and only then moved into the line, so another waiter
// never sees it unlocked and removes it as a dead waiter's.
func joinQueue(slotsDirectory string) (*queueTicket, error) {
	queue := queueDirectory(slotsDirectory)
	if createDirectoryError := os.MkdirAll(queue, 0o755); createDirectoryError != nil {
		return nil, createDirectoryError
	}
	name := fmt.Sprintf("%020d-%d", time.Now().UnixNano(), os.Getpid())
	pending := filepath.Join(queue, "."+name)
	file, openError := os.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if openError != nil {
		return nil, openError
	}
	// A lock that fails and a lock not taken are two answers: only the first has an error to wrap, and a fresh
	// O_EXCL file that someone else already holds is a different fault worth its own words.
	taken, tryLockFileError := tryLockFile(file)
	if tryLockFileError != nil || !taken {
		// Ignored: the ticket was never written, joining fails with the lock's error either way, and a pending
		// ticket left behind is named with a dot, which no look at the line counts.
		_ = file.Close()
		_ = os.Remove(pending) // Ignored: as the close above.
	}
	if tryLockFileError != nil {
		return nil, fmt.Errorf("locking a new queue ticket: %w", tryLockFileError)
	}
	if !taken {
		return nil, fmt.Errorf("locking a new queue ticket: %s was made by this run and is locked by another", pending)
	}
	path := filepath.Join(queue, name)
	if renameError := os.Rename(pending, path); renameError != nil {
		unlockFile(file)
		// Ignored: the ticket was never written, joining fails with the rename's error either way, and a pending
		// ticket left behind is named with a dot, which no look at the line counts.
		_ = file.Close()
		_ = os.Remove(pending) // Ignored: as the close above.
		return nil, renameError
	}
	return &queueTicket{file: file, path: path}, nil
}

func queueDirectory(slotsDirectory string) string {
	return filepath.Join(slotsDirectory, "queue")
}

// place counts the live tickets ahead of this one, and every live ticket in line with it. A ticket whose
// lock can be taken is a waiter that is gone, however it ended, and is removed on the way.
func (ticket *queueTicket) place() (ahead int, waiting int, placeError error) {
	queue := filepath.Dir(ticket.path)
	// Sorted by name, which is arrival order.
	entries, readDirectoryError := os.ReadDir(queue)
	if readDirectoryError != nil {
		return 0, 0, readDirectoryError
	}
	mine := filepath.Base(ticket.path)
	for _, entry := range entries {
		name := entry.Name()
		if name == mine {
			waiting++
			continue
		}
		// A ticket still being made is not in line yet.
		if name[0] == '.' {
			continue
		}
		path := filepath.Join(queue, name)
		other, openError := os.OpenFile(path, os.O_RDWR, 0)
		if openError != nil {
			// Removed since the listing, by its waiter taking a slot or by another waiter's cleanup.
			continue
		}
		// Ignored: a ticket whose lock can't be tested counts as a live waiter, the side on which no one in line
		// is passed over or removed.
		taken, _ := tryLockFile(other)
		if taken {
			// Ignored: a dead waiter's ticket that stays is passed over by every look, as here, and the next
			// look removes it.
			_ = os.Remove(path)
			unlockFile(other)
			_ = other.Close() // Ignored: opened only to test its lock, and never written.
			continue
		}
		_ = other.Close() // Ignored: opened only to test its lock, and never written.
		waiting++
		if name < mine {
			ahead++
		}
	}
	return ahead, waiting, nil
}

// leave takes the ticket out of line, once its run holds a slot or gives up waiting.
func (ticket *queueTicket) leave() {
	// Ignored: a ticket left in line once its lock is dropped below is a dead waiter's, which the next look at
	// the line passes over and removes.
	_ = os.Remove(ticket.path)
	unlockFile(ticket.file)
	_ = ticket.file.Close() // Ignored: never written, and its lock goes with the descriptor.
}

// ordinal is a place in line as a person says it: 1st, 2nd, 3rd, 4th, 11th, 12th, 13th, 21st.
func ordinal(place int) string {
	suffix := "th"
	if place%100 < 11 || place%100 > 13 {
		switch place % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", place, suffix)
}
