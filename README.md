# Tillit

Tillit validates and verifies the commits on a Git feature branch without modifying the branch, index, or working files.

## Quick start

From a feature branch with commits ahead of its base:

```sh
go install ./cmd/tillit
tillit config set validate.command '["go","test","./..."]'
tillit start
tillit status
tillit feedback --json
```

Use `tillit start --foreground` to stream daemon output to your terminal.
Run `tillit stop` to stop the daemon in either mode.

## How checks run

The daemon selects commits using the branch's upstream or the default branch's fork point.
Validation runs a configured command in a separate detached Git worktree.
Passes and failures are cached by exact commit and effective configuration.
The daemon retries infrastructure errors.
`feedback` reports cached failures and command output without rerunning checks.
Direct `go test` commands report individual test, subtest, and build failures using Go's JSON events.
Other commands report their failures at the command level.
For supported Go test arguments, the daemon reruns failing tests with an anchored `-run` pattern in one package and records the command, output, and outcome.
If the isolated run passes, Tillit may rerun the original test selection in just that package.
Reruns are bounded and never change the original validation result.

Set shared configuration with `tillit config set --global` or `tillit config set --repository`.
Without a scope flag, changes go to the current worktree's Git directory.
A committed `.tillit/config.json` takes precedence for the commit being checked.
Run `tillit config --help` for configuration commands.

Verification runs independently of validation.
It currently verifies certain local variable renames in single-file, import-free Go changes under the assumptions in [CONTEXT.md](CONTEXT.md).
A passing validation does not imply verification.
