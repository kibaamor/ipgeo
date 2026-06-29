//go:build !windows

package downloader

import "os"

func replaceFile(srcPath, destPath string) error {
	return os.Rename(srcPath, destPath)
}
