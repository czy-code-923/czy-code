//go:build !linux && !windows

package hostmeter

import "errors"

func readNative() (string, []Raw, error) {
	return "", nil, errors.New("host traffic requires Linux or Windows")
}
