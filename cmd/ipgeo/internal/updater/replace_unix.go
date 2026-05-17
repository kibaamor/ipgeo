//go:build !windows

package updater

import "os"

func replaceBinary(newPath, execPath string) (bool, error) {
	return false, os.Rename(newPath, execPath)
}

func replaceFile(newPath, destPath string) error {
	return os.Rename(newPath, destPath)
}
