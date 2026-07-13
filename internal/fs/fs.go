package fs

import (
	"github.com/spf13/afero"
)

// FS is an interface wrapping afero.Fs to allow for easy mocking and extension if needed.
type FS interface {
	afero.Fs
}

// defaultFS is the default implementation using afero.OsFs.
type defaultFS struct {
	afero.Fs
}

// New returns a new file system backed by the OS.
func New() FS {
	return &defaultFS{
		Fs: afero.NewOsFs(),
	}
}

// NewMemory returns a new in-memory file system, primarily useful for testing.
func NewMemory() FS {
	return &defaultFS{
		Fs: afero.NewMemMapFs(),
	}
}
