//go:build windows

package action

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLockFile 以 LockFileEx(独占 + 立即失败) 取非阻塞独占锁。
func tryLockFile(f *os.File) error {
	const (
		lockfileExclusiveLock   = 2
		lockfileFailImmediately = 1
	)
	var overlapped windows.Overlapped
	// 锁整个文件范围 (0xFFFFFFFF): 只做互斥用, 不关心内容。
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		lockfileExclusiveLock|lockfileFailImmediately,
		0, 0xFFFFFFFF, 0xFFFFFFFF, &overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errors.New("lock is held by another process")
	}
	return err
}

// unlockFile 释放 LockFileEx 持有的范围锁。
func unlockFile(f *os.File) {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 0xFFFFFFFF, 0xFFFFFFFF, &overlapped)
}
