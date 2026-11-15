# Spor

- Spor should track application state in a SQLite database in the Git worktree's git-dir, under a spor subfolder.
- Spor should use `pi` to call a coding agent for specified coding agent tasks using a configured provider/model.
- Spor configuration should follow a hierarchy:
  - Global configuration in `~/.config/spor/config.json`
  - Repository configuration in `spor/config.json` in the Git worktree's git-common-dir (that's the base worktree's git-dir, right?)
  - Worktree configuration in `spor/config.json` in the Git worktree's git-dir

## Skills

Load these skills before working in this directory or its descendants.

- `go-standards` when writing, modifying, fixing, or refactoring Go.
- `go-testify` when writing or changing tests that use testify.
- `audit-tests` when writing, changing, reviewing, or sweeping tests.
- `constable` when checking Go packages or repairing a diagnostic it reports.
- `mise` when installing tools, changing `mise.toml`, or running project tasks.
- `markdown-standards` when writing or editing Markdown.
- `tom` before reading Go source to assess the repository's structure.
- `go-template` when changing the repository tooling baseline, mise tasks, Git hooks, or GitHub CI.
- `hk-configure` when adding or changing linters, formatters, or steps in `hk.pkl`.
- `hk-debug` when an hk check or Git hook fails, or a step runs on the wrong files.
- `bash-standards` when writing or reviewing Bash, including shell in mise tasks.
- `domain-modeling` when changing terminology, `CONTEXT.md`, or an ADR.
- `requirement-levels` when writing or interpreting requirement keywords in these instructions.

Load the `go-modern-*` skill that matches each API in the change.
Load these when that API is part of the change:

- `go-modern-errors-is`, `go-modern-errors-as-type`, and `go-modern-errors-join` when handling errors.
- `go-modern-testing-t-context` when a test needs a context.
- `go-modern-context-cancel-cause`, `go-modern-context-after-func`, and `go-modern-context-timeout-deadline-cause` when work must stop with a cause, cleanup, or a deadline.
- `go-modern-sync-waitgroup-go` when a `sync.WaitGroup` tracks goroutines.
- `go-modern-json-v2` and `go-modern-json-omitzero` when reading or writing JSON, including Spor configuration.

## Subagents

Use gpt-6.1-sol on high effort for planning and delegate all work to a subagent running gpt-6.1-terra on medium effort.
