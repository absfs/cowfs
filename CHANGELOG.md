# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Thread-safe operations with mutex protection for concurrent access
- Deletion tracking to properly hide removed files from primary filesystem
- Copy-on-write support for metadata operations (Chmod, Chtimes, Chown)
- Comprehensive test suite with 95.7% code coverage
- Race condition tests for concurrent operations
- Example functions demonstrating usage patterns
- Benchmark suite for performance testing
- golangci-lint configuration for code quality
- CONTRIBUTING.md with development guidelines
- CHANGELOG.md for tracking changes

### Changed
- FileSystem is now safe for concurrent use by multiple goroutines
- Remove() now properly tracks deletions and prevents reads from primary
- Metadata operations (Chmod, Chtimes, Chown) now copy files to secondary before modification
- Improved error handling in OpenFile copy logic

### Fixed
- Race conditions when accessing modified/deleted maps
- Files not being properly copied from primary to secondary during metadata operations
- Removed files still being accessible from primary filesystem

## [0.0.1] - 2018

### Added
- Initial implementation of Copy-on-Write FileSystem
- Basic absfs.Filer interface implementation
- Support for OpenFile, Mkdir, Remove, Rename, Stat operations
- README with usage examples
- MIT License

[Unreleased]: https://github.com/absfs/cowfs/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/absfs/cowfs/releases/tag/v0.0.1
