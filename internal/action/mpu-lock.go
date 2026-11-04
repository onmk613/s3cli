package action

import (
	"fmt"
	"os"
	"path/filepath"
)

// mpuLock 是已持有的跨进程锁。Release 幂等。
type mpuLock struct {
	f *os.File
}

// Release 释放锁并删除锁文件 (持锁期间删除会被 unix 语义优雅处理:
// 锁在 fd 关闭时释放, 文件删除只影响后续打开者)。
func (l *mpuLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	unlockFile(l.f)
	_ = l.f.Close()
	_ = os.Remove(l.f.Name())
	l.f = nil
}

// lockMultipartState 对 statePath 对应的上传目标取非阻塞独占锁。
// 锁被其他进程持有时返回可读的错误。
func lockMultipartState(statePath string) (*mpuLock, error) {
	lockPath := statePath + ".lock"
	// 锁先于状态文件存在: 首次上传时状态目录还没被 saveMultipartState 创建。
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create mpu state dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open mpu lock %s: %w", lockPath, err)
	}
	if err := tryLockFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another s3cli process is already uploading this file to the same bucket/key (lock %s held): %w", lockPath, err)
	}
	return &mpuLock{f: f}, nil
}
