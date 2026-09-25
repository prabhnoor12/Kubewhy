# Contributing to kubewhy

Thanks for your interest in improving kubewhy. This guide covers the workflow and conventions used in the project.

## How to contribute

### Reporting bugs

Open a [GitHub Issue](https://github.com/kubewhy/kubewhy/issues) and include:

- kubewhy version (`kubewhy version` or the release tag)
- Kubernetes version (`kubectl version`)
- The pod JSON or request JSON that reproduces the problem
- The actual output vs. the output you expected

### Requesting features

Open an issue describing the scenario: what you were debugging, what information you needed, and why the current diagnosis didn't provide it. This helps us evaluate whether the feature belongs in the core engine or as an extension.

### Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your changes — keep commits focused and atomic.
3. Add or update tests for any logic change.
4. Make sure the existing suite passes:
   ```bash
   gofmt -l cmd internal
   go vet ./...
   go test -race ./...
   ```
5. Open a pull request against `main` and describe what changed and why.

If your change is large or architectural, open an issue first so we can align on the approach before you spend time on code.

## Development setup

```bash
git clone https://github.com/kubewhy/kubewhy.git
cd kubewhy
go test ./...
go run ./cmd/kubewhy diagnose --file examples/crashloop-request.json
```

No external services or cluster access are needed to run the test suite. The diagnosis engine is a pure function — it takes structured input and returns a report.

## Code conventions

- **Standard Go style.** Follow `gofmt` and `go vet` without exceptions.
- **Table-driven tests.** Each diagnosis check should have test cases covering the positive match, the negative (no match), and edge cases.
- **No panics in library code.** Return errors. The CLI and API layers handle error presentation.
- **Keep dependencies minimal.** Every new `go.mod` dependency is a burden on every consumer. Prefer the standard library when it solves the problem.
- **Read-only by default.** The collector must never mutate cluster state. If a new data source is added, it must be read-only.

## Adding a new diagnosis check

The diagnosis engine (`internal/diagnosis/engine.go`) is the most common place for contributions. Each check follows the same pattern:

1. **Inspect** a field from the `DiagnoseRequest` (pod status, events, logs, or resources).
2. **Match** a condition that indicates a problem.
3. **Produce** a `Reason` with a stable `code`, a human-readable `title`, an `explanation`, `evidence` strings, and `remediation` steps.
4. **Assign** a `severity** (`critical`, `error`, or `warning`) and a `confidence` (`high`, `medium`, or `low`).

The engine ranks reasons by causal weight — root causes rank above symptoms. When adding a check, consider whether it describes a root cause or a downstream effect, and set the weight accordingly.

Example structure:

```go
func checkSomething(req *model.DiagnoseRequest) []model.Reason {
    var reasons []model.Reason
    for _, c := range req.Pod.Status.ContainerStatuses {
        if /* condition */ {
            reasons = append(reasons, model.Reason{
                Code:        "your_code",
                Severity:    "error",
                Confidence:  "high",
                Title:       "Human-readable title",
                Explanation: "Why this matters.",
                Evidence:    []string{"field=value"},
                Remediation: []string{"What to do about it"},
            })
        }
    }
    return reasons
}
```

Add a corresponding test in `engine_test.go` with at least three cases: match, no-match, and edge case.

## Commit messages

Use imperative mood: "Add check for X", "Fix log parsing for Y", "Update README with Z". Keep the first line under 72 characters. Add a body when the "why" isn't obvious from the "what".

## Code review

Every pull request needs at least one approval before merging. CI must pass (format, vet, test, lint). Reviewers will look for:

- Correctness of the diagnosis logic
- Test coverage of new checks
- No unnecessary dependencies
- Clear error messages and remediation steps

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
