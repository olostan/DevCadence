//go:build windows

package setup

import (
	"os"
)

func lockShared(f *os.File) error {
	return nil
}

func lockExclusive(f *os.File) error {
	return nil
}

func unlock(f *os.File) error {
	return nil
}
