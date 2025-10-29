# Contributing to bd

Thank you for your interest in contributing to bd! This document provides guidelines and instructions for contributing.

## Development Setup

### Prerequisites

- Go 1.25 or later
- Git
- (Optional) golangci-lint for local linting

### Getting Started

```bash
# Clone the repository
git clone https://github.com/steveyegge/beads
cd beads

# Build the project
go build -o bd ./cmd/bd

# Run tests
go test ./...

# Run with race detection
go test -race ./...

# Build and install locally
go install ./cmd/bd
```

## Project Structure

```
beads/
├── cmd/bd/              # CLI entry point and commands
├── internal/
│   ├── types/           # Core data types (Issue, Dependency, etc.)
│   └── storage/         # Storage interface and implementations
│       └── sqlite/      # SQLite backend
├── .golangci.yml        # Linter configuration
└── .github/workflows/   # CI/CD pipelines
```

## Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package tests
go test ./internal/storage/sqlite -v

# Run tests with race detection
go test -race ./...
```

## Code Style

We follow standard Go conventions:

- Use `gofmt` to format your code (runs automatically in most editors)
- Follow the [Effective Go](https://golang.org/doc/effective_go) guidelines
- Keep functions small and focused
- Write clear, descriptive variable names
- Add comments for exported functions and types

### Linting

We use golangci-lint for code quality checks:

```bash
# Install golangci-lint
brew install golangci-lint  # macOS
# or
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run ./...
```

**Note**: The linter currently reports ~100 warnings. These are documented false positives and idiomatic Go patterns (deferred cleanup, Cobra interface requirements, etc.). See [docs/LINTING.md](docs/LINTING.md) for details. When contributing, focus on avoiding *new* issues rather than the baseline warnings.

CI will automatically run linting on all pull requests.

## Making Changes

### Workflow

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes
4. Add tests for new functionality
5. Run tests and linter locally
6. Commit your changes with clear messages
7. Push to your fork
8. Open a pull request

### Commit Messages

Write clear, concise commit messages:

```
Add cycle detection for dependency graphs

- Implement recursive CTE-based cycle detection
- Add tests for simple and complex cycles
- Update documentation with examples
```

### Pull Requests

- Keep PRs focused on a single feature or fix
- Include tests for new functionality
- Update documentation as needed
- Ensure CI passes before requesting review
- Respond to review feedback promptly

## Testing Guidelines

- Write table-driven tests when testing multiple scenarios
- Use descriptive test names that explain what is being tested
- Clean up resources (database files, etc.) in test teardown
- Use `t.Run()` for subtests to organize related test cases

Example:

```go
func TestIssueValidation(t *testing.T) {
    tests := []struct {
        name    string
        issue   *types.Issue
        wantErr bool
    }{
        {
            name:    "valid issue",
            issue:   &types.Issue{Title: "Test", Status: types.StatusOpen, Priority: 2},
            wantErr: false,
        },
        {
            name:    "missing title",
            issue:   &types.Issue{Status: types.StatusOpen, Priority: 2},
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.issue.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

## Documentation

- Update README.md for user-facing changes
- Update relevant .md files in the project root
- Add inline code comments for complex logic
- Include examples in documentation

## Feature Requests and Bug Reports

### Reporting Bugs

Include in your bug report:
- Steps to reproduce
- Expected behavior
- Actual behavior
- Version of bd (`bd version` if implemented)
- Operating system and Go version

### Feature Requests

When proposing new features:
- Explain the use case
- Describe the proposed solution
- Consider backwards compatibility
- Discuss alternatives you've considered

## Code Review Process

All contributions go through code review:

1. Automated checks (tests, linting) must pass
2. At least one maintainer approval required
3. Address review feedback
4. Maintainer will merge when ready

## Development Tips

### Testing Locally

```bash
# Build and test your changes quickly
go build -o bd ./cmd/bd && ./bd init --prefix test

# Test specific functionality
./bd create "Test issue" -p 1 -t bug
./bd dep add test-2 test-1
./bd ready
```

### Database Inspection

```bash
# Inspect the SQLite database directly
sqlite3 .beads/test.db

# Useful queries
SELECT * FROM issues;
SELECT * FROM dependencies;
SELECT * FROM events WHERE issue_id = 'test-1';
```

### Debugging

Use Go's built-in debugging tools:

```bash
# Run with verbose logging
go run ./cmd/bd -v create "Test"

# Use delve for debugging
dlv debug ./cmd/bd -- create "Test issue"
```

## Release Process

(For maintainers)

1. Update version in code
2. Update CHANGELOG.md
3. Tag release: `git tag v0.x.0`
4. Push tag: `git push origin v0.x.0`
5. GitHub Actions will build and publish

## Questions?

- Check existing [issues](https://github.com/steveyegge/beads/issues)
- Open a new issue for questions
- Review [README.md](README.md) and other documentation

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Code of Conduct

Be respectful and professional in all interactions. We're here to build something great together.

---

Thank you for contributing to bd! 🎉
