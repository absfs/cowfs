# Contributing to CowFS

Thank you for your interest in contributing to CowFS! This document provides guidelines and information for contributors.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/yourusername/cowfs.git`
3. Create a feature branch: `git checkout -b feature/your-feature-name`

## Development Setup

### Prerequisites

- Go 1.21 or later
- golangci-lint (optional but recommended)

### Building

```bash
go build -v ./...
```

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run with race detector
go test -v -race ./...

# Check coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Running Benchmarks

```bash
go test -bench=. -benchmem
```

### Linting

```bash
golangci-lint run
```

## Code Guidelines

### Code Style

- Follow standard Go conventions
- Run `gofmt` before committing
- Use meaningful variable and function names
- Add comments for exported types and functions

### Testing

- Maintain or improve test coverage (currently 95.7%)
- Add tests for all new functionality
- Include both positive and negative test cases
- Test edge cases and error conditions
- Ensure all tests pass with race detector

### Commit Messages

- Use clear, descriptive commit messages
- Start with a verb in present tense (e.g., "Add", "Fix", "Update")
- Reference issues when applicable (e.g., "Fix #123")

Example:
```
Add support for directory operations

- Implement ReadDir functionality
- Add tests for directory listing
- Update documentation

Fixes #42
```

## Pull Request Process

1. **Before submitting:**
   - Ensure all tests pass: `go test -v -race ./...`
   - Run linter: `golangci-lint run`
   - Update documentation if needed
   - Add/update tests for your changes

2. **PR Description:**
   - Clearly describe what changes you made and why
   - Reference related issues
   - Include any breaking changes

3. **Review Process:**
   - Maintainers will review your PR
   - Address any feedback or requested changes
   - Once approved, your PR will be merged

## Reporting Issues

### Bug Reports

When reporting bugs, please include:
- Go version
- Operating system
- Steps to reproduce
- Expected behavior
- Actual behavior
- Code sample (if applicable)

### Feature Requests

For feature requests, please describe:
- The use case
- Proposed solution
- Any alternatives considered

## Questions?

Feel free to open an issue for questions or discussions about the project.

## License

By contributing to CowFS, you agree that your contributions will be licensed under the MIT License.
