//go:build linux || darwin

package source

import "syscall"

// diskUsage returns the free (available to unprivileged users) and total
// bytes of the filesystem that holds path.
func diskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize, nil
}
