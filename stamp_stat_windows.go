//go:build windows

package main

import "os"

// StatStamp returns a stamp for the given file path.
//
// On Windows we only reliably record mtime, size, and Go's file mode.
func StatStamp(path string) (FileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileStamp{}, err
	}

	return FileStamp{
		MTimeUnixNano: fi.ModTime().UnixNano(),
		Size:          fi.Size(),
		Mode:          uint32(fi.Mode()),
	}, nil
}
