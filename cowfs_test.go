package cowfs

import (
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

// mockFiler is a minimal mock implementation for testing
type mockFiler struct {
	files map[string]*mockFile
	mu    sync.Mutex
}

func newMockFiler() *mockFiler {
	return &mockFiler{files: make(map[string]*mockFile)}
}

func (m *mockFiler) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if f, ok := m.files[name]; ok {
		// Reset offset for reading
		f.mu.Lock()
		f.offset = 0
		f.mu.Unlock()
		return f, nil
	}
	if flag&os.O_CREATE == 0 {
		return nil, os.ErrNotExist
	}
	f := &mockFile{name: name, data: []byte{}, mode: perm}
	m.files[name] = f
	return f, nil
}

func (m *mockFiler) Mkdir(name string, perm os.FileMode) error { return nil }
func (m *mockFiler) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, name)
	return nil
}
func (m *mockFiler) Rename(oldpath, newpath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[oldpath]; ok {
		m.files[newpath] = f
		delete(m.files, oldpath)
		return nil
	}
	return os.ErrNotExist
}
func (m *mockFiler) Stat(name string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[name]; ok {
		f.mu.Lock()
		info := &mockFileInfo{name: name, size: int64(len(f.data)), mode: f.mode}
		f.mu.Unlock()
		return info, nil
	}
	return nil, os.ErrNotExist
}
func (m *mockFiler) Chmod(name string, mode os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[name]; ok {
		f.mu.Lock()
		f.mode = mode
		f.mu.Unlock()
		return nil
	}
	return os.ErrNotExist
}
func (m *mockFiler) Chtimes(name string, atime, mtime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		return nil
	}
	return os.ErrNotExist
}
func (m *mockFiler) Chown(name string, uid, gid int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		return nil
	}
	return os.ErrNotExist
}

type mockFile struct {
	name   string
	data   []byte
	offset int64
	mode   os.FileMode
	mu     sync.Mutex
}

func (f *mockFile) Name() string { return f.name }
func (f *mockFile) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(b, f.data[f.offset:])
	f.offset += int64(n)
	return n, nil
}
func (f *mockFile) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append(f.data, b...)
	return len(b), nil
}
func (f *mockFile) Close() error                                 { return nil }
func (f *mockFile) Seek(offset int64, whence int) (int64, error) { return 0, nil }
func (f *mockFile) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &mockFileInfo{name: f.name, size: int64(len(f.data)), mode: f.mode}, nil
}
func (f *mockFile) Sync() error                             { return nil }
func (f *mockFile) Readdir(n int) ([]os.FileInfo, error)    { return nil, nil }
func (f *mockFile) Readdirnames(n int) ([]string, error)    { return nil, nil }
func (f *mockFile) ReadAt(b []byte, off int64) (int, error) { return 0, nil }
func (f *mockFile) WriteAt(b []byte, off int64) (int, error) {
	return len(b), nil
}
func (f *mockFile) WriteString(s string) (int, error) { return len(s), nil }
func (f *mockFile) Truncate(size int64) error         { return nil }

type mockFileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mockFileInfo) IsDir() bool        { return false }
func (m *mockFileInfo) Sys() interface{}   { return nil }

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

	// Check file is marked as deleted
	if !fs.deleted["/test.txt"] {
		t.Error("File not marked as deleted")
	}
}

func TestRemoveBlocksPrimaryRead(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("primary")}

	// Remove the file
	err := fs.Remove("/test.txt")
	if err != nil {
		t.Errorf("Remove() error = %v", err)
	}

	// Try to read the file - should get ErrNotExist
	_, err = fs.OpenFile("/test.txt", os.O_RDONLY, 0)
	if err != os.ErrNotExist {
		t.Errorf("Expected os.ErrNotExist after Remove, got %v", err)
	}
}

func TestRename(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/old.txt"] = &mockFile{name: "/old.txt", data: []byte("content"), mode: 0644}

	// Rename the file
	err := fs.Rename("/old.txt", "/new.txt")
	if err != nil {
		t.Errorf("Rename() error = %v", err)
	}

	// Check new file is marked as modified
	if !fs.modified["/new.txt"] {
		t.Error("New file not marked as modified after Rename")
	}

	// Check old file is marked as deleted
	if !fs.deleted["/old.txt"] {
		t.Error("Old file not marked as deleted after Rename")
	}

	// Verify file was copied to secondary before rename
	if _, ok := secondary.files["/new.txt"]; !ok {
		t.Error("File not found in secondary after Rename")
	}
}

func TestStat(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Stat should read from primary
	info, err := fs.Stat("/test.txt")
	if err != nil {
		t.Errorf("Stat() error = %v", err)
	}
	if info == nil {
		t.Fatal("Stat() returned nil info")
	}
}

func TestStatDeleted(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Remove the file
	fs.Remove("/test.txt")

	// Stat should return ErrNotExist
	_, err := fs.Stat("/test.txt")
	if err != os.ErrNotExist {
		t.Errorf("Expected os.ErrNotExist for deleted file, got %v", err)
	}
}

func TestChmod(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Change mode
	err := fs.Chmod("/test.txt", 0755)
	if err != nil {
		t.Errorf("Chmod() error = %v", err)
	}

	// Check file is marked as modified
	if !fs.modified["/test.txt"] {
		t.Error("File not marked as modified after Chmod")
	}

	// Verify file was copied to secondary
	if _, ok := secondary.files["/test.txt"]; !ok {
		t.Error("File not copied to secondary before Chmod")
	}

	// Verify mode was changed in secondary
	if secondary.files["/test.txt"].mode != 0755 {
		t.Errorf("Expected mode 0755, got %v", secondary.files["/test.txt"].mode)
	}
}

func TestChtimes(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Change times
	now := time.Now()
	err := fs.Chtimes("/test.txt", now, now)
	if err != nil {
		t.Errorf("Chtimes() error = %v", err)
	}

	// Check file is marked as modified
	if !fs.modified["/test.txt"] {
		t.Error("File not marked as modified after Chtimes")
	}

	// Verify file was copied to secondary
	if _, ok := secondary.files["/test.txt"]; !ok {
		t.Error("File not copied to secondary before Chtimes")
	}
}

func TestChown(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Change owner
	err := fs.Chown("/test.txt", 1000, 1000)
	if err != nil {
		t.Errorf("Chown() error = %v", err)
	}

	// Check file is marked as modified
	if !fs.modified["/test.txt"] {
		t.Error("File not marked as modified after Chown")
	}

	// Verify file was copied to secondary
	if _, ok := secondary.files["/test.txt"]; !ok {
		t.Error("File not copied to secondary before Chown")
	}
}

func TestOpenFileReadModified(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("primary"), mode: 0644}

	// Write to the file (modifies it)
	f, _ := fs.OpenFile("/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	f.Write([]byte("modified"))
	f.Close()

	// Read should now come from secondary
	f2, err := fs.OpenFile("/test.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer f2.Close()

	// The file should have both primary and modified content
	buf := make([]byte, 100)
	n, _ := f2.Read(buf)
	content := string(buf[:n])
	if content != "primarymodified" {
		t.Errorf("Expected 'primarymodified', got '%s'", content)
	}
}

func TestOpenFileDeleted(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("primary"), mode: 0644}

	// Remove the file
	fs.Remove("/test.txt")

	// Try to open - should get ErrNotExist
	_, err := fs.OpenFile("/test.txt", os.O_RDONLY, 0)
	if err != os.ErrNotExist {
		t.Errorf("Expected os.ErrNotExist for deleted file, got %v", err)
	}
}

func TestOpenFileRecreateAfterDelete(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Remove non-existent file
	fs.Remove("/test.txt")

	// Create the file
	f, err := fs.OpenFile("/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	f.Close()

	// File should not be deleted anymore
	if fs.deleted["/test.txt"] {
		t.Error("File still marked as deleted after recreation")
	}

	// File should be marked as modified
	if !fs.modified["/test.txt"] {
		t.Error("File not marked as modified after creation")
	}
}

func TestConcurrentAccess(t *testing.T) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	// Add file to primary
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}

	// Test concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			// Try various operations
			fs.Stat("/test.txt")
			fs.OpenFile("/test.txt", os.O_RDONLY, 0)
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Example demonstrates basic usage of cowfs
func Example() {
	primary := newMockFiler()
	secondary := newMockFiler()

	// Add a file to the primary filesystem
	primary.files["/config.txt"] = &mockFile{
		name: "/config.txt",
		data: []byte("original content"),
		mode: 0644,
	}

	// Create copy-on-write filesystem
	fs := New(primary, secondary)

	// Read from primary
	f, _ := fs.OpenFile("/config.txt", os.O_RDONLY, 0)
	buf := make([]byte, 100)
	n, _ := f.Read(buf)
	f.Close()
	fmt.Println(string(buf[:n]))

	// Write creates a copy in secondary
	w, _ := fs.OpenFile("/config.txt", os.O_CREATE|os.O_WRONLY, 0644)
	w.Write([]byte(" modified"))
	w.Close()

	// Subsequent reads come from secondary
	f2, _ := fs.OpenFile("/config.txt", os.O_RDONLY, 0)
	buf2 := make([]byte, 100)
	n2, _ := f2.Read(buf2)
	f2.Close()
	fmt.Println(string(buf2[:n2]))

	// Primary remains unchanged
	fmt.Println(string(primary.files["/config.txt"].data))
	// Output:
	// original content
	// original content modified
	// original content
}

// ExampleFileSystem_Remove demonstrates deletion tracking
func ExampleFileSystem_Remove() {
	primary := newMockFiler()
	secondary := newMockFiler()

	// Add a file to the primary filesystem
	primary.files["/file.txt"] = &mockFile{
		name: "/file.txt",
		data: []byte("content"),
		mode: 0644,
	}

	fs := New(primary, secondary)

	// Remove the file
	fs.Remove("/file.txt")

	// File appears deleted even though it's still in primary
	_, err := fs.OpenFile("/file.txt", os.O_RDONLY, 0)
	fmt.Println(err == os.ErrNotExist)
	// Output: true
}

// ExampleFileSystem_Chmod demonstrates copy-on-write for metadata operations
func ExampleFileSystem_Chmod() {
	primary := newMockFiler()
	secondary := newMockFiler()

	// Add a file to the primary filesystem
	primary.files["/file.txt"] = &mockFile{
		name: "/file.txt",
		data: []byte("content"),
		mode: 0644,
	}

	fs := New(primary, secondary)

	// Changing mode triggers copy to secondary
	fs.Chmod("/file.txt", 0755)

	// File is now in secondary with new mode
	info, _ := secondary.Stat("/file.txt")
	fmt.Println(info.Mode().Perm() == 0755)

	// Primary remains unchanged
	fmt.Println(primary.files["/file.txt"].mode == 0644)
	// Output:
	// true
	// true
}

// Benchmarks

func BenchmarkOpenFileRead(b *testing.B) {
	primary := newMockFiler()
	secondary := newMockFiler()
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}
	fs := New(primary, secondary)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := fs.OpenFile("/test.txt", os.O_RDONLY, 0)
		f.Close()
	}
}

func BenchmarkOpenFileWrite(b *testing.B) {
	primary := newMockFiler()
	secondary := newMockFiler()
	fs := New(primary, secondary)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := fs.OpenFile("/test.txt", os.O_CREATE|os.O_WRONLY, 0644)
		f.Write([]byte("data"))
		f.Close()
	}
}

func BenchmarkStat(b *testing.B) {
	primary := newMockFiler()
	secondary := newMockFiler()
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}
	fs := New(primary, secondary)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.Stat("/test.txt")
	}
}

func BenchmarkChmod(b *testing.B) {
	primary := newMockFiler()
	secondary := newMockFiler()
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}
	fs := New(primary, secondary)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fs.Chmod("/test.txt", 0755)
	}
}

func BenchmarkConcurrentReads(b *testing.B) {
	primary := newMockFiler()
	secondary := newMockFiler()
	primary.files["/test.txt"] = &mockFile{name: "/test.txt", data: []byte("content"), mode: 0644}
	fs := New(primary, secondary)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f, _ := fs.OpenFile("/test.txt", os.O_RDONLY, 0)
			f.Close()
		}
	})
}
