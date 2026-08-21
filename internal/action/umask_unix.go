//go:build !windows

// umask_unix.go 提供下载文件权限放宽所需的 umask 读取 (unix)。

package action

import (
	"os"
	"sync"
	"syscall"
)

// chmodDownloaded 把临时文件权限从 os.CreateTemp 的 0600 放宽为
// "0666 & ^umask" (与常规新建文件一致, 通常 0644)。
func chmodDownloaded(path string) {
	mode := os.FileMode(0o666 &^ currentUmask())
	_ = os.Chmod(path, mode)
}

// currentUmask 读取进程 umask (读取动作会临时改写, 立即恢复), 仅做一次。
var currentUmask = sync.OnceValue(func() int {
	u := syscall.Umask(0)
	syscall.Umask(u)
	return u
})
