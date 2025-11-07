package cowfs

import (
	"os"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

// mockFiler is a minimal mock implementation for testing
type mockFiler struct {
	files map[string]*mockFile
}

func newMockFiler() *mockFiler {
	return &mockFiler{files: make(map[string]*mockFile)}
}

func (m *mockFiler) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	if f, ok := m.files[name]; ok {
		return f, nil
	}
	if flag&os.O_CREATE == 0 {
		return nil, os.ErrNotExist
	}
	f := &mockFile{name: name, data: []byte{}}
	m.files[name] = f
	return f, nil
}

func (m *mockFiler) Mkdir(name string, perm os.FileMode) error       { return nil }
func (m *mockFiler) Remove(name string) error                        { delete(m.files, name); return nil }
func (m *mockFiler) Rename(oldpath, newpath string) error            { return nil }
func (m *mockFiler) Stat(name string) (os.FileInfo, error)           { return nil, os.ErrNotExist }
func (m *mockFiler) Chmod(name string, mode os.FileMode) error       { return nil }
func (m *mockFiler) Chtimes(name string, atime, mtime time.Time) error { return nil }
func (m *mockFiler) Chown(name string, uid, gid int) error           { return nil }

type mockFile struct {
	name   string
	data   []byte
	offset int64
}

func (f *mockFile) Name() string                                 { return f.name }
func (f *mockFile) Read(b []byte) (int, error)                   { return copy(b, f.data), nil }
func (f *mockFile) Write(b []byte) (int, error)                  { f.data = append(f.data, b...); return len(b), nil }
func (f *mockFile) Close() error                                 { return nil }
func (f *mockFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *mockFile) Stat() (os.FileInfo, error)                   { return nil, nil }
func (f *mockFile) Sync() error                                  { return nil }
func (f *mockFile) Readdir(n int) ([]os.FileInfo, error)         { return nil, nil }
func (f *mockFile) Readdirnames(n int) ([]string, error)         { return nil, nil }
func (f *mockFile) ReadAt(b []byte, off int64) (int, error)      { return 0, nil }
func (f *mockFile) WriteAt(b []byte, off int64) (int, error)     { return len(b), nil }
func (f *mockFile) WriteString(s string) (int, error)            { return len(s), nil }
func (f *mockFile) Truncate(size int64) error                    { return nil }

func TestNew(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()

	fs := New(primary, secondary)
	if fs == nil {
		t.Fatal("New() returned nil")
	}
	if fs.primary != primary {
		t.Error("primary filesystem not set correctly")
	}
	if fs.secondary != secondary {
		t.Error("secondary filesystem not set correctly")
	}
}

func TestOpenFileRead(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add a file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("primary")}

	f, err := fs.OpenFile("/test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if f == nil {
		t.Fatal("OpenFile() returned nil file")
	}
	f.Close()
}

func TestOpenFileWrite(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	f, err := fs.OpenFile("/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if f == nil {
		t.Fatal("OpenFile() returned nil file")
	}
	f.Close()

	// Check that file is marked as modified
	if !fs.modified["/test.txt"] {
		t.Error("File not marked as modified after write")
	}
}

func TestMkdir(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	err := fs.Mkdir("/testdir", 0755)
	if err != nil {
		t.Errorf("Mkdir() error = %v", err)
	}
}

func TestRemove(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	err := fs.Remove("/test.txt")
	if err != nil {
		t.Errorf("Remove() error = %v", err)
	}
}
