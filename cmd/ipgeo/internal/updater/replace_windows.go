//go:build windows

package updater

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReplaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func replaceBinary(newPath, execPath string) (bool, error) {
	if err := os.Rename(newPath, execPath); err == nil {
		return false, nil
	}
	newPathPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return false, fmt.Errorf("encode new path: %w", err)
	}
	execPathPtr, err := windows.UTF16PtrFromString(execPath)
	if err != nil {
		return false, fmt.Errorf("encode executable path: %w", err)
	}
	err = windows.MoveFileEx(
		newPathPtr,
		execPathPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_DELAY_UNTIL_REBOOT,
	)
	return err == nil, err
}

func replaceFile(newPath, destPath string) error {
	if err := os.Rename(newPath, destPath); err == nil {
		return nil
	}

	destPathPtr, err := windows.UTF16PtrFromString(destPath)
	if err != nil {
		return fmt.Errorf("encode destination path: %w", err)
	}
	newPathPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return fmt.Errorf("encode new path: %w", err)
	}

	r1, _, callErr := procReplaceFileW.Call(
		uintptr(unsafe.Pointer(destPathPtr)),
		uintptr(unsafe.Pointer(newPathPtr)),
		0,
		0,
		0,
		0,
	)
	if r1 == 0 {
		return callErr
	}
	return nil
}
