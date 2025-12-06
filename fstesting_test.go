package cowfs_test

import (
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/cowfs"
	"github.com/absfs/fstesting"
	"github.com/absfs/memfs"
)

// TestCowFS_WrapperSuite runs the fstesting wrapper suite for cowfs.
// cowfs is a copy-on-write wrapper that combines a primary (read-only) and secondary (writable) filesystem.
func TestCowFS_WrapperSuite(t *testing.T) {
	// Create an in-memory base filesystem for testing
	baseFS, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	// Factory creates a cowfs wrapper around the base filesystem
	factory := func(base absfs.FileSystem) (absfs.FileSystem, error) {
		// Create a separate secondary filesystem for writes
		secondaryFS, err := memfs.NewFS()
		if err != nil {
			return nil, err
		}

		// cowfs.New returns a *cowfs.FileSystem which implements absfs.Filer
		// We need to extend it to absfs.FileSystem
		cowFilesystem := cowfs.New(base, secondaryFS)
		return absfs.ExtendFiler(cowFilesystem), nil
	}

	suite := &fstesting.WrapperSuite{
		Factory:        factory,
		BaseFS:         baseFS,
		Name:           "cowfs",
		TransformsData: false, // cowfs passes data through unchanged
		TransformsMeta: false, // cowfs preserves metadata
		ReadOnly:       false, // cowfs supports write operations (to secondary)
	}

	suite.Run(t)
}

// TestCowFS_Suite runs the full fstesting suite for cowfs.
// This tests cowfs as a complete filesystem implementation.
func TestCowFS_Suite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full suite in short mode")
	}

	// Create primary and secondary filesystems
	primary, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	secondary, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	// Create cowfs and extend to FileSystem
	cowFilesystem := cowfs.New(primary, secondary)
	fs := absfs.ExtendFiler(cowFilesystem)

	// Configure features based on what cowfs supports
	// cowfs delegates to underlying filesystems (memfs in this case)
	// so it supports what memfs supports
	features := fstesting.Features{
		Symlinks:      false, // memfs doesn't support symlinks
		HardLinks:     false, // memfs doesn't support hard links
		Permissions:   true,  // memfs supports permissions
		Timestamps:    true,  // memfs supports timestamps
		CaseSensitive: true,  // memfs is case-sensitive
		AtomicRename:  true,  // memfs has atomic rename
		SparseFiles:   false, // memfs doesn't support sparse files
		LargeFiles:    true,  // memfs supports large files
	}

	suite := &fstesting.Suite{
		FS:       fs,
		Features: features,
	}

	suite.Run(t)
}

// TestCowFS_QuickCheck runs a quick sanity check.
func TestCowFS_QuickCheck(t *testing.T) {
	// Create primary and secondary filesystems
	primary, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	secondary, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	// Create cowfs and extend to FileSystem
	cowFilesystem := cowfs.New(primary, secondary)
	fs := absfs.ExtendFiler(cowFilesystem)

	suite := &fstesting.Suite{
		FS: fs,
		Features: fstesting.Features{
			Permissions:   true,
			Timestamps:    true,
			CaseSensitive: true,
			AtomicRename:  true,
			LargeFiles:    true,
		},
	}

	suite.QuickCheck(t)
}
