//go:build windows

// umask_windows.go: Windows 不支持细粒度 unix 权限位, 下载文件保持默认权限。

package action

func chmodDownloaded(string) {}
