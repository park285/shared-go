//go:build windows

package workercontract

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openRegularProfile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, profileOpenError(err)
	}

	// Reparse point 자체를 열어 최종 symlink/junction을 따라가지 않는다.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, profileOpenError(err)
	}

	kind, err := windows.GetFileType(handle)
	if err != nil {
		return nil, profileOpenError(errors.Join(err, windows.CloseHandle(handle)))
	}
	if kind != windows.FILE_TYPE_DISK {
		return nil, profileOpenError(errors.Join(profileFileError{code: ProfileFileTypeInvalid}, windows.CloseHandle(handle)))
	}

	var info windows.ByHandleFileInformation

	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, profileOpenError(errors.Join(err, windows.CloseHandle(handle)))
	}

	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return nil, profileOpenError(errors.Join(profileFileError{code: ProfileFileTypeInvalid}, windows.CloseHandle(handle)))
	}

	return os.NewFile(uintptr(handle), path), nil
}
