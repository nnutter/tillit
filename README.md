# Spor

Spor watches tracked working changes in a Git worktree, extracts supported Go refactorings into commits, and runs a configured check before publishing them to the current branch.

## Automatic refactoring

Configure an operational check, then start the watcher:

```sh
spor config set auditor.validate.command '["go", "test", "./..."]'
spor start
spor status
```

Use `spor start --foreground` to run in the terminal, and `spor stop` to stop a background watcher.
The watcher polls once per second and waits for two matching observations before capturing a snapshot.
It uses an alternate index and excludes untracked files.

Spor extracts supported refactorings without committing the remaining feature changes.
It validates each exact candidate commit in an isolated checkout, and publishes only if every candidate passes the configured check.
Passing the check is operational validation, not proof of semantic equivalence.
Spor does not generate fixup commits or verify or validate existing feature-branch commits; use tillit for that workflow.

Publication requires an attached branch, a clean index, and no Git operation in progress.
Sparse, skip-worktree, and assume-unchanged index entries are unsupported.
Spor rechecks the branch, tip, index, and working snapshot before publication.
Changes made during validation cause the candidate to be skipped.
A failed or skipped snapshot is not repeatedly submitted while its inputs remain unchanged.

Publication advances the branch and replaces the index with the validated tree, leaving working files untouched.
The refactor disappears from the uncommitted diff while unrelated changes remain.
A journal supports recovery after interrupted publication, but external Git commands and editor writes cannot be made atomic with publication.
Avoid changing branches or running Git mutations at the instant Spor publishes.

If an older Spor version left manual commits pending or an integration interrupted, automatic publication is blocked.
Use the previous Spor version to finish or undo that work, or resolve it safely before continuing.
Spor does not delete the old refs or state.

A background daemon writes output to `spor/daemon.log` under the worktree Git directory and rotates it at 5 MiB, keeping three backups.
The log is readable only by its owner and can contain command output.
Follow it across rotation with `tail -F` on the path printed by `spor start`.
Foreground output goes to stderr.

## Configuration

Spor reads configuration in increasing priority order, replacing only fields present in each file:

1. Global: `~/.config/spor/config.json`.
1. Repository: `spor/config.json` in the Git common directory.
1. Worktree: `spor/config.json` in the worktree Git directory, when distinct from the common directory.
1. Worktree root: `.spor/config.json` at the worktree root.

The `spor config` write commands target the worktree Git directory by default, or the selected global or repository scope; they do not write the worktree root file.
`auditor.validate.command` is the program and arguments to run, for example:

```json
{
  "auditor": {
    "validate": {
      "command": ["go", "test", "./..."]
    }
  }
}
```

Existing configuration files containing legacy settings remain accepted, but those settings do not enable the old workflows.
If a file uses `auditor.verify`, move that object to `auditor.validate` before running Spor.
