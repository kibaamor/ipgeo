//go:build windows

package downloader

import "golang.org/x/sys/windows"

func replaceFile(srcPath, destPath string) error {
	src, err := windows.UTF16PtrFromString(srcPath)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(destPath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dest, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
