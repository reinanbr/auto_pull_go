# Contributing to autopull

Thanks for taking the time to contribute.

## Development setup

Requires Go 1.21+.

```bash
git clone git@github.com:reinanbr/autopull.git
cd autopull
go build -o autopull .
go test ./...
```

Project layout is documented in the [Development](README.md#development) section of the README.

## Workflow

1. Fork the repo and create a branch from `main`.
2. Make your change. Keep it focused — unrelated fixes belong in a separate PR.
3. Add or update tests in `tests/` for any behavior change.
4. Run the checks below before opening a PR.
5. Open a pull request describing the change and the reasoning behind it.

## Checks before submitting

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```

CI runs the same checks (build, vet, test, module tidy check) on every PR.

## Code style

- Standard `gofmt` formatting, no linter-specific exceptions.
- Keep the core watcher logic in `autopull/`; `main.go` stays a thin CLI dispatcher.
- Prefer clear, explicit error messages — this tool runs unattended as a daemon, so errors are often read from logs, not a terminal.
- Avoid new external dependencies; the project is intentionally zero-dependency Go stdlib only.

## Commit messages

Short, imperative summary line (`fix: ...`, `feat: ...`, `docs: ...`, `ci: ...`) matching the style in `git log`. Explain *why* in the body when the change isn't self-evident from the diff.

## Reporting bugs / requesting features

Open a GitHub issue with:
- What you expected vs. what happened.
- Your config (with tokens redacted) and relevant log lines from `autopull.log`.
- OS/architecture and `autopull --version` output.

## Releasing

Maintainers only: bump `version` in `main.go`, add a `CHANGELOG.md` entry, then tag:

```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin main --tags
```

Pushing a `v*` tag triggers the release workflow in `.github/workflows/ci.yml`, which builds and publishes the Linux release artifacts.
