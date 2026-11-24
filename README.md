# Tillit

Tillit validates and verifies the commits on a Git feature branch without modifying the branch, index, or working files.

## Quick start

From a feature branch with commits ahead of its base:

```sh
go install ./cmd/tillit
tillit config set auditor.validate.command '["go","test","./..."]'
tillit start
tillit status
```

Run `tillit stop` to stop the background daemon, or use `tillit daemon` to run in the foreground.

## How checks run

The daemon selects commits using the branch's upstream or the default branch's fork point.
Validation runs a configured command in a separate detached Git worktree.
Passes and failures are cached by exact commit and effective configuration.
The daemon retries infrastructure errors.

Set shared configuration with `tillit config set --global` or `tillit config set --repository`.
Without a scope flag, changes go to the current worktree's Git directory.
A committed `.tillit/config.json` takes precedence for the commit being checked.
Run `tillit config --help` for configuration commands.

Verification runs independently of validation.
It currently verifies certain local variable renames in single-file, import-free Go changes under the assumptions in [CONTEXT.md](CONTEXT.md).
A passing validation does not imply verification.
