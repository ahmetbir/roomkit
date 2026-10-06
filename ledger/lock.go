//go:build unix

package ledger

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockName is the ledger's exclusive lock in its directory. Two servers on
// one volume (blue/green deploys) never write the journal at once: the
// second Open fails with ErrLocked until the first has closed. flock is
// released by the kernel when the process dies, so a crash leaves no stale
// lock.
const lockName = "stats.lock"

// ErrLocked: another ledger (process) has the directory open.
var ErrLocked = errors.New("ledger: directory locked by another store")

func lockDir(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return f, nil
}
