//go:build !windows

package main

import (
	"os"
	"syscall"
)

// StatStamp returns a stamp for the given file path.
//
// On Unix-like systems it includes inode, mode, uid, and gid.
func StatStamp(path string) (FileStamp, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileStamp{}, err
	}

	stamp := FileStamp{
		MTimeUnixNano: fi.ModTime().UnixNano(),
		Size:          fi.Size(),
		Mode:          uint32(fi.Mode()),
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return stamp, nil
	}

	stamp.Inode = uint64(st.Ino)
	stamp.Mode = uint32(st.Mode)
	stamp.UID = uint32(st.Uid)
	stamp.GID = uint32(st.Gid)
	return stamp, nil
}
