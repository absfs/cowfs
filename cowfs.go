// Package cowfs implements a Copy-on-Write FileSystem that wraps two absfs.Filer
// implementations. It reads from a primary read-only filesystem and directs all
// writes and modifications to a secondary writable filesystem, leaving the primary
// unchanged.
package cowfs

import (
	"os"
	"time"

	"github.com/absfs/absfs"
)

// FileSystem implements absfs.Filer with copy-on-write semantics.
// Reads come from the primary filesystem, while writes and modifications
// go to the secondary filesystem.
type FileSystem struct {
	primary   absfs.Filer // Primary read-only filesystem
	secondary absfs.Filer // Secondary writable filesystem
	modified  map[string]bool // Track which files have been modified
}

// New creates a new CowFS that reads from primary and writes to secondary.
func New(primary, secondary absfs.Filer) *FileSystem {
	return &FileSystem{
		primary:   primary,
		secondary: secondary,
		modified:  make(map[string]bool),
	}
}

// OpenFile opens a file, reading from primary or secondary based on modification state.
// Write operations mark files as modified and direct them to secondary.
func (fs *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	// If writing or creating, use secondary
	if flag&(os.O_CREATE|os.O_WRONLY|os.O_RDWR|os.O_TRUNC|os.O_APPEND) != 0 {
		fs.modified[name] = true
		// Try to copy from primary if it exists and we're not truncating
		if flag&os.O_TRUNC == 0 {
			if primaryFile, err := fs.primary.OpenFile(name, os.O_RDONLY, 0); err == nil {
				// Create in secondary and copy content
				secondaryFile, err := fs.secondary.OpenFile(name, os.O_CREATE|os.O_WRONLY, perm)
				if err == nil {
					buf := make([]byte, 32*1024)
					for {
						n, readErr := primaryFile.Read(buf)
						if n > 0 {
							secondaryFile.Write(buf[:n])
						}
						if readErr != nil {
							break
						}
					}
					secondaryFile.Close()
				}
				primaryFile.Close()
			}
		}
		return fs.secondary.OpenFile(name, flag, perm)
	}

	// For read-only access, check if file has been modified
	if fs.modified[name] {
		return fs.secondary.OpenFile(name, flag, perm)
	}

	// Try primary first, fallback to secondary
	file, err := fs.primary.OpenFile(name, flag, perm)
	if err != nil {
		return fs.secondary.OpenFile(name, flag, perm)
	}
	return file, nil
}

// Mkdir creates a directory in the secondary filesystem.
func (fs *FileSystem) Mkdir(name string, perm os.FileMode) error {
	fs.modified[name] = true
	return fs.secondary.Mkdir(name, perm)
}

// Remove removes a file from the secondary filesystem and marks it.
func (fs *FileSystem) Remove(name string) error {
	fs.modified[name] = true
	return fs.secondary.Remove(name)
}

// Rename renames a file in the secondary filesystem.
func (fs *FileSystem) Rename(oldpath, newpath string) error {
	fs.modified[oldpath] = true
	fs.modified[newpath] = true
	return fs.secondary.Rename(oldpath, newpath)
}

// Stat returns file info, checking secondary first if modified.
func (fs *FileSystem) Stat(name string) (os.FileInfo, error) {
	if fs.modified[name] {
		return fs.secondary.Stat(name)
	}
	info, err := fs.primary.Stat(name)
	if err != nil {
		return fs.secondary.Stat(name)
	}
	return info, nil
}

// Chmod changes the mode in the secondary filesystem.
func (fs *FileSystem) Chmod(name string, mode os.FileMode) error {
	fs.modified[name] = true
	return fs.secondary.Chmod(name, mode)
}

// Chtimes changes the times in the secondary filesystem.
func (fs *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	fs.modified[name] = true
	return fs.secondary.Chtimes(name, atime, mtime)
}

// Chown changes the owner in the secondary filesystem.
func (fs *FileSystem) Chown(name string, uid, gid int) error {
	fs.modified[name] = true
	return fs.secondary.Chown(name, uid, gid)
}
