//go:build !(linux || darwin)

package source

import "errors"

func diskUsage(string) (free, total uint64, err error) {
	return 0, 0, errors.New("disk usage is not supported on this platform")
}
