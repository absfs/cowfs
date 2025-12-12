// Package cowfs implements a Copy-on-Write FileSystem that wraps two absfs.Filer
// implementations. It reads from a primary read-only filesystem and directs all
// writes and modifications to a secondary writable filesystem, leaving the primary
// unchanged.
package cowfs

import (
	"io"
	"io/fs"
	"os"
	"path"
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
		file, err := fs.secondary.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		// Check if directory - wrap for merging
		if info, statErr := file.Stat(); statErr == nil && info.IsDir() {
			return &mergedDirFile{
				File:      file,
				name:      name,
				fs:        fs,
				primary:   fs.primary,
				secondary: fs.secondary,
			}, nil
		}
		return file, nil
	}

	// Try primary first, fallback to secondary
	file, err := fs.primary.OpenFile(name, flag, perm)
	if err != nil {
		file, err = fs.secondary.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		// Check if directory from secondary - wrap for merging
		if info, statErr := file.Stat(); statErr == nil && info.IsDir() {
			return &mergedDirFile{
				File:      file,
				name:      name,
				fs:        fs,
				primary:   fs.primary,
				secondary: fs.secondary,
			}, nil
		}
		return file, nil
	}

	// Check if this is a directory from primary - wrap for merging
	info, statErr := file.Stat()
	if statErr == nil && info.IsDir() {
		return &mergedDirFile{
			File:      file,
			name:      name,
			fs:        fs,
			primary:   fs.primary,
			secondary: fs.secondary,
		}, nil
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

// Truncate truncates a file to the specified size.
// If the file exists only in primary, it's copied to secondary first.
func (fs *FileSystem) Truncate(name string, size int64) error {
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

	// Now truncate in secondary
	f, err := fs.secondary.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}

// ReadDir reads the named directory and returns a list of directory entries.
func (cfs *FileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	cfs.mu.RLock()
	isDeleted := cfs.deleted[name]
	isModified := cfs.modified[name]
	cfs.mu.RUnlock()

	if isDeleted {
		return nil, os.ErrNotExist
	}

	// If the directory was modified, read from secondary
	if isModified {
		return cfs.secondary.ReadDir(name)
	}

	// Try primary first
	entries, err := cfs.primary.ReadDir(name)
	if err != nil {
		// Fallback to secondary
		return cfs.secondary.ReadDir(name)
	}

	// Filter deleted entries and merge with secondary
	var result []fs.DirEntry
	seen := make(map[string]bool)

	for _, entry := range entries {
		entryPath := path.Join(name, entry.Name())
		cfs.mu.RLock()
		isDeleted := cfs.deleted[entryPath]
		cfs.mu.RUnlock()

		if !isDeleted {
			result = append(result, entry)
			seen[entry.Name()] = true
		}
	}

	// Add entries from secondary that aren't in primary
	secondaryEntries, err := cfs.secondary.ReadDir(name)
	if err == nil {
		for _, entry := range secondaryEntries {
			if !seen[entry.Name()] {
				entryPath := path.Join(name, entry.Name())
				cfs.mu.RLock()
				isDeleted := cfs.deleted[entryPath]
				cfs.mu.RUnlock()

				if !isDeleted {
					result = append(result, entry)
				}
			}
		}
	}

	return result, nil
}

// ReadFile reads the named file and returns its contents.
func (cfs *FileSystem) ReadFile(name string) ([]byte, error) {
	cfs.mu.RLock()
	isDeleted := cfs.deleted[name]
	isModified := cfs.modified[name]
	cfs.mu.RUnlock()

	if isDeleted {
		return nil, os.ErrNotExist
	}

	// If the file was modified, read from secondary
	if isModified {
		return cfs.secondary.ReadFile(name)
	}

	// Try primary first
	data, err := cfs.primary.ReadFile(name)
	if err != nil {
		// Fallback to secondary
		return cfs.secondary.ReadFile(name)
	}

	return data, nil
}

// TempDir returns the temp directory path from the secondary (writable) filesystem.
// This implements the optional temper interface so that ExtendFiler can delegate
// to the appropriate filesystem.
func (cfs *FileSystem) TempDir() string {
	// Check if secondary implements temper
	type temper interface {
		TempDir() string
	}
	if t, ok := cfs.secondary.(temper); ok {
		return t.TempDir()
	}
	// Fallback to /tmp
	return "/tmp"
}

// Sub returns a Filer corresponding to the subtree rooted at dir.
func (cfs *FileSystem) Sub(dir string) (fs.FS, error) {
	return absfs.FilerToFS(cfs, dir)
}

// mergedDirFile wraps a directory File to merge listings from primary and secondary
// filesystems while filtering deleted entries.
type mergedDirFile struct {
	absfs.File
	name      string
	fs        *FileSystem
	primary   absfs.Filer
	secondary absfs.Filer
	merged    []os.FileInfo // Cached merged result
	offset    int           // Current read position in merged
}

// Readdir reads directory entries, merging from both primary and secondary
// while filtering deleted files.
func (f *mergedDirFile) Readdir(n int) ([]os.FileInfo, error) {
	if f.merged == nil {
		if err := f.buildMerged(); err != nil {
			return nil, err
		}
	}

	if n <= 0 {
		// Return all remaining entries
		result := f.merged[f.offset:]
		f.offset = len(f.merged)
		if len(result) == 0 {
			return result, io.EOF
		}
		return result, nil
	}

	// Return up to n entries
	if f.offset >= len(f.merged) {
		return nil, io.EOF
	}

	end := f.offset + n
	if end > len(f.merged) {
		end = len(f.merged)
	}

	result := f.merged[f.offset:end]
	f.offset = end

	if f.offset >= len(f.merged) && len(result) < n {
		return result, io.EOF
	}

	return result, nil
}

// Readdirnames reads directory entry names, merging from both filesystems.
func (f *mergedDirFile) Readdirnames(n int) ([]string, error) {
	infos, err := f.Readdir(n)
	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}
	return names, err
}

// ReadDir reads directory entries, merging from both primary and secondary
// while filtering deleted files. Returns fs.DirEntry values.
func (f *mergedDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	// Use the filesystem-level ReadDir for proper merging
	return f.fs.ReadDir(f.name)
}

// buildMerged constructs the merged directory listing.
func (f *mergedDirFile) buildMerged() error {
	seen := make(map[string]bool)
	var result []os.FileInfo

	// Get entries from primary
	primaryFile, err := f.primary.OpenFile(f.name, os.O_RDONLY, 0)
	if err == nil {
		primaryEntries, _ := primaryFile.Readdir(-1)
		primaryFile.Close()

		for _, entry := range primaryEntries {
			// Skip . and .. entries
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}

			// Use path.Join for virtual filesystem paths (always uses /)
			entryPath := path.Join(f.name, name)

			// Skip if deleted in overlay
			f.fs.mu.RLock()
			isDeleted := f.fs.deleted[entryPath]
			f.fs.mu.RUnlock()

			if !isDeleted {
				result = append(result, entry)
				seen[name] = true
			}
		}
	}

	// Get entries from secondary (only new/modified ones not in primary)
	secondaryFile, err := f.secondary.OpenFile(f.name, os.O_RDONLY, 0)
	if err == nil {
		secondaryEntries, _ := secondaryFile.Readdir(-1)
		secondaryFile.Close()

		for _, entry := range secondaryEntries {
			// Skip . and .. entries
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}

			if !seen[name] {
				// Use path.Join for virtual filesystem paths (always uses /)
				entryPath := path.Join(f.name, name)

				// Skip if marked as deleted
				f.fs.mu.RLock()
				isDeleted := f.fs.deleted[entryPath]
				f.fs.mu.RUnlock()

				if !isDeleted {
					result = append(result, entry)
				}
			}
		}
	}

	f.merged = result
	return nil
}

// emptyFiler is a minimal Filer that always returns ErrNotExist.
// It's used as a placeholder when a sub-filesystem doesn't have a corresponding directory.
type emptyFiler struct{}

func (e *emptyFiler) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	return nil, os.ErrNotExist
}

func (e *emptyFiler) Mkdir(name string, perm os.FileMode) error {
	return os.ErrNotExist
}

func (e *emptyFiler) Remove(name string) error {
	return os.ErrNotExist
}

func (e *emptyFiler) Rename(oldpath, newpath string) error {
	return os.ErrNotExist
}

func (e *emptyFiler) Stat(name string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func (e *emptyFiler) Chmod(name string, mode os.FileMode) error {
	return os.ErrNotExist
}

func (e *emptyFiler) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return os.ErrNotExist
}

func (e *emptyFiler) Chown(name string, uid, gid int) error {
	return os.ErrNotExist
}

func (e *emptyFiler) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, os.ErrNotExist
}

func (e *emptyFiler) ReadFile(name string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (e *emptyFiler) Sub(dir string) (fs.FS, error) {
	return &emptyFS{}, nil
}

// emptyFS is a minimal fs.FS that always returns ErrNotExist.
type emptyFS struct{}

func (e *emptyFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}
