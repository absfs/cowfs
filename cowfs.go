// Package cowfs implements a Copy-on-Write FileSystem that wraps two absfs.Filer
// implementations. It reads from a primary read-only filesystem and directs all
// writes and modifications to a secondary writable filesystem, leaving the primary
// unchanged.
package cowfs

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// FileSystem implements absfs.Filer with copy-on-write semantics.
// Reads come from the primary filesystem, while writes and modifications
// go to the secondary filesystem. FileSystem is safe for concurrent use.
type FileSystem struct {
	primary   absfs.Filer     // Primary read-only filesystem
	secondary absfs.Filer     // Secondary writable filesystem
	mu        sync.RWMutex    // Protects modified and deleted maps
	modified  map[string]bool // Track which files have been modified
	deleted   map[string]bool // Track which files have been deleted
}

// New creates a new CowFS that reads from primary and writes to secondary.
func New(primary, secondary absfs.Filer) *FileSystem {
	return &FileSystem{
		primary:   primary,
		secondary: secondary,
		modified:  make(map[string]bool),
		deleted:   make(map[string]bool),
	}
}

// OpenFile opens a file, reading from primary or secondary based on modification state.
// Write operations mark files as modified and direct them to secondary.
func (fs *FileSystem) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	// If writing or creating, use secondary
	if flag&(os.O_CREATE|os.O_WRONLY|os.O_RDWR|os.O_TRUNC|os.O_APPEND) != 0 {
		fs.mu.Lock()
		alreadyInSecondary := fs.modified[name]
		fs.modified[name] = true
		delete(fs.deleted, name) // Undelete if recreating
		fs.mu.Unlock()

		// Try to copy from primary if it exists, not already in secondary, and we're not truncating
		if !alreadyInSecondary && flag&os.O_TRUNC == 0 {
			if primaryFile, err := fs.primary.OpenFile(name, os.O_RDONLY, 0); err == nil {
				// Create in secondary and copy content
				secondaryFile, err := fs.secondary.OpenFile(name, os.O_CREATE|os.O_WRONLY, perm)
				if err == nil {
					_, copyErr := io.Copy(secondaryFile, primaryFile)
					secondaryFile.Close()
					if copyErr != nil {
						primaryFile.Close()
						return nil, copyErr
					}
				}
				primaryFile.Close()
			}
		}
		return fs.secondary.OpenFile(name, flag, perm)
	}

	// For read-only access, check if file has been deleted
	fs.mu.RLock()
	isDeleted := fs.deleted[name]
	isModified := fs.modified[name]
	fs.mu.RUnlock()

	if isDeleted {
		return nil, os.ErrNotExist
	}

	// For read-only access, check if file has been modified
	if isModified {
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
	fs.mu.Lock()
	fs.modified[name] = true
	delete(fs.deleted, name)
	fs.mu.Unlock()
	return fs.secondary.Mkdir(name, perm)
}

// Remove removes a file from the secondary filesystem and marks it as deleted.
func (fs *FileSystem) Remove(name string) error {
	fs.mu.Lock()
	fs.deleted[name] = true
	delete(fs.modified, name)
	fs.mu.Unlock()

	// Try to remove from secondary if it exists there
	_ = fs.secondary.Remove(name)
	return nil
}

// Rename renames a file in the secondary filesystem.
func (fs *FileSystem) Rename(oldpath, newpath string) error {
	fs.mu.Lock()
	wasModified := fs.modified[oldpath]
	fs.deleted[oldpath] = true
	delete(fs.modified, oldpath)
	fs.modified[newpath] = true
	delete(fs.deleted, newpath)
	fs.mu.Unlock()

	// If file wasn't in secondary, copy from primary first
	if !wasModified {
		if primaryFile, err := fs.primary.OpenFile(oldpath, os.O_RDONLY, 0); err == nil {
			secondaryFile, err := fs.secondary.OpenFile(oldpath, os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				io.Copy(secondaryFile, primaryFile)
				secondaryFile.Close()
			}
			primaryFile.Close()
		}
	}

	return fs.secondary.Rename(oldpath, newpath)
}

// Stat returns file info, checking secondary first if modified.
func (fs *FileSystem) Stat(name string) (os.FileInfo, error) {
	fs.mu.RLock()
	isDeleted := fs.deleted[name]
	isModified := fs.modified[name]
	fs.mu.RUnlock()

	if isDeleted {
		return nil, os.ErrNotExist
	}

	if isModified {
		return fs.secondary.Stat(name)
	}
	info, err := fs.primary.Stat(name)
	if err != nil {
		return fs.secondary.Stat(name)
	}
	return info, nil
}

// Chmod changes the mode in the secondary filesystem.
// If the file exists only in primary, it's copied to secondary first.
func (fs *FileSystem) Chmod(name string, mode os.FileMode) error {
	fs.mu.Lock()
	wasModified := fs.modified[name]
	fs.modified[name] = true
	fs.mu.Unlock()

	// If file wasn't in secondary, copy from primary first
	if !wasModified {
		if primaryFile, err := fs.primary.OpenFile(name, os.O_RDONLY, 0); err == nil {
			stat, _ := primaryFile.Stat()
			perm := os.FileMode(0644)
			if stat != nil {
				perm = stat.Mode().Perm()
			}
			secondaryFile, err := fs.secondary.OpenFile(name, os.O_CREATE|os.O_WRONLY, perm)
			if err == nil {
				io.Copy(secondaryFile, primaryFile)
				secondaryFile.Close()
			}
			primaryFile.Close()
		}
	}

	return fs.secondary.Chmod(name, mode)
}

// Chtimes changes the times in the secondary filesystem.
// If the file exists only in primary, it's copied to secondary first.
func (fs *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	fs.mu.Lock()
	wasModified := fs.modified[name]
	fs.modified[name] = true
	fs.mu.Unlock()

	// If file wasn't in secondary, copy from primary first
	if !wasModified {
		if primaryFile, err := fs.primary.OpenFile(name, os.O_RDONLY, 0); err == nil {
			stat, _ := primaryFile.Stat()
			perm := os.FileMode(0644)
			if stat != nil {
				perm = stat.Mode().Perm()
			}
			secondaryFile, err := fs.secondary.OpenFile(name, os.O_CREATE|os.O_WRONLY, perm)
			if err == nil {
				io.Copy(secondaryFile, primaryFile)
				secondaryFile.Close()
			}
			primaryFile.Close()
		}
	}

	return fs.secondary.Chtimes(name, atime, mtime)
}

// Chown changes the owner in the secondary filesystem.
// If the file exists only in primary, it's copied to secondary first.
func (fs *FileSystem) Chown(name string, uid, gid int) error {
	fs.mu.Lock()
	wasModified := fs.modified[name]
	fs.modified[name] = true
	fs.mu.Unlock()

	// If file wasn't in secondary, copy from primary first
	if !wasModified {
		if primaryFile, err := fs.primary.OpenFile(name, os.O_RDONLY, 0); err == nil {
			stat, _ := primaryFile.Stat()
			perm := os.FileMode(0644)
			if stat != nil {
				perm = stat.Mode().Perm()
			}
			secondaryFile, err := fs.secondary.OpenFile(name, os.O_CREATE|os.O_WRONLY, perm)
			if err == nil {
				io.Copy(secondaryFile, primaryFile)
				secondaryFile.Close()
			}
			primaryFile.Close()
		}
	}

	return fs.secondary.Chown(name, uid, gid)
}
