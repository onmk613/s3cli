//go:build !windows

package action

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile 以 flock(LOCK_EX|LOCK_NB) 取非阻塞独占锁。
// flock 按 open file description 计锁: 同进程用另一个 fd 也会冲突,
// 与跨进程语义一致, 便于测试。
func tryLockFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return errors.New("lock is held by another process")
	}
	return err
}

// unlockFile 释放 flock; fd 关闭时内核也会自动释放, 这里显式解锁只是
// 让语义完整 (锁文件随 Release 一并删除)。
func unlockFile(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
