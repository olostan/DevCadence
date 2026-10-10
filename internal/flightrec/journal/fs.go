package journal

import (
	"errors"
	"io"
	"os"
)

// FS is the filesystem seam used for everything except the LOCK file (which
// needs the real descriptor for flock). Production code uses OSFS; tests inject
// faults.
type FS interface {
	MkdirAll(path string, perm os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	SyncDir(path string) error
	ReadDir(path string) ([]os.DirEntry, error)
	Lstat(path string) (os.FileInfo, error)
	Open(name string) (ReadFile, error)
}

// File is an append-only segment handle.
type File interface {
	io.Writer
	Sync() error
	Close() error
	Stat() (os.FileInfo, error)
}

// ReadFile is a read-only segment handle used by the scanner.
type ReadFile interface {
	io.ReaderAt
	Stat() (os.FileInfo, error)
	Close() error
}

type osFS struct{}

// OSFS returns the operating-system filesystem.
func OSFS() FS { return osFS{} }

func (osFS) MkdirAll(p string, perm os.FileMode) error { return os.MkdirAll(p, perm) }

func (osFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// SyncDir fsyncs a directory so that a created entry is durable.
func (osFS) SyncDir(p string) error {
	d, err := os.Open(p)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func (osFS) ReadDir(p string) ([]os.DirEntry, error) { return os.ReadDir(p) }

func (osFS) Lstat(p string) (os.FileInfo, error) { return os.Lstat(p) }

func (osFS) Open(name string) (ReadFile, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	return f, nil
}
