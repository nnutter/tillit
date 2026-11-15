# Spor

Spor tracks your changes, turns them into a coherent Git history, verifies every commit, and automatically makes fixup commits, as needed.

Spor will work in an adjacent branch/worktree to verify and fix-up commits, when you're ready, it will rebase those fixup commits back into your original branch.

Spor will analyze the staged changes to detect and extract refactor commits.

## Example Workflow

| User | User Branch | Spor Branch | Spor |
| --- | --- | --- | --- |
| User runs `spor start` |  |  | Spor spawns a daemon |
| User makes changes | \* |  |  |
| User stages changes | \* |  |  |
| User runs `spor add` | \* |  |  |
| User continues making changes | \* |  | Spor receives staged changes |
|  |  |  | Spor analyzes staged changes and extracts 3 commits, A, B, and C, to Spor Index |
|  |  | A | Spor checks out A to Spor Branch and begins verification |
|  |  | A - A! | Verification fails, Spor makes a fixup commit, and restarts verfiication |
|  |  | A - A! - B | Verification succeeds, Spor pulls in commit B and starts verification |
|  |  | A - A! - B - C | Verification succeeds, Spor pulls in commit C and starts verification |
|  |  | A - A! - B - C - C! | Verification fails, Spor makes a fixup commit, and restarts verfiication |
| User runs `spor rebase` | A - A! - B - \* |  |  |
| User commits `git commit` | A - A! - B - D |  |  |
|  |  | A - A! - B - C - C! | Verification succeeds |
| User runs `spor rebase` | A - A! - B - D - C - C! - \* |  |  |

Spor's branch before the first rebase.

![Spor branch before the first rebase](docs/diagrams/git-before-rebase.svg)

After the first rebase, the user commits D.

![After the first rebase, the user commits D](docs/diagrams/git-after-first-rebase.svg)

After the second rebase, Spor's branch matches the user's.

![After the second rebase, both branches match](docs/diagrams/git-after-second-rebase.svg)

- `A`
  - `Change-Id: C1`
  - `Spor-Id: S1`
- `A'`
  - `Change-Id: C2`
  - `Fixup-Id: C1-1`
- `B`
  - `Change-Id: C3`
  - `Spor-Id: S1`
- `C`
  - `Change-Id: C4`
  - `Spor-Id: S1`
- `C'`
  - `Change-Id: C5`
  - `Fixup-Id: C4-1`

## Spor Commands

- `spor start` - Starts Spor daemon in the current Git worktree.
- `spor status` - Displays the status of the Spor daemon and any pending changes.
- `spor add` - Adds the staged changes to Spor for processing.
  `spor stage` is an alias for `spor add`.
- `spor reset` - Undoes the latest `spor add`.
- `spor log` - Shows added commits that are not yet on the user's branch with verification status.
- `spor rebase` - Tells the daemon (Mender) to rebase into/under the User's branch/worktree.
- `spor verify` - Verifies spor commits. `--mode` overrides the configured mode for that run.
- `spor stop` - Stops the Spor daemon.

## Change Tracking

Spor will add a Change-Id Git commit message trailer to identify commits similar to what Gerrit Code Review does.
Spor will also track fixup commits with a Fixup-Id Git commit message trailer to identify commits that are fixups of other Change-Id.
Spor will add a Spor-Id Git commit message trailer to identify changes that were committed to Spor.

## Daemon Components

- Smith: The Smith will extract commits from the added changes and maintain an isolated Git Index to build commits.
- Auditor: The Auditor will verify commits and make fixup commits for any that fail verification.
- Mender: The Mender will incorporate Spor commits back into the User's branch/worktree.

## Milestones

### 0.1.0

- Spor will automatically extract and commit basic refactor commits from your staged changes.
- `spor add` writes those commits and leaves the branch, index, and worktree alone.
- `spor status` shows the latest submission.

### 0.2.0

- Spor will automatically verify commits.
- The verification modes are commit, patch, and change, and the default mode is change.
- `spor add` verifies unverified commits from the oldest forward and stops at the first failure.
- `spor verify` checks again, and `--mode` overrides the configured mode for that run.
- `spor status` shows the verification outcome.

### 0.3.0

- Spor will automatically make fixup commits for commits that fail verification.

### 0.4.0

- Spor will rebase fixup commits back into your original branch at your request.

### 0.5.0

- Spor could also monitor GitHub PR statuses.
  However, the goal is to keep Spor as a local tool, so this would be an extension of it, not superseding local verification.

## Future Ideas

Spor would be able to chat with you wherever you work, whether that's in your IDE, in a terminal, or in an instant messenger.

Spor would automatically extract and commit quasi-refactor commits from your staged changes.
Quasi-refactor commits are commits that are not strictly refactors, but are still simple enough and self-contained enough do not having a user-facing change.

Spor would also provide code review and suggest commits rather than automatically making them.

Tightly integrate something like hk into the verification process.

## Usage

Spor reads configuration from three files.
A later file replaces only the fields it sets.
Spor skips a file that is absent.
The mode defaults to `change`.

- Global configuration is `~/.config/spor/config.json`.
- Repository configuration is `spor/config.json` in the Git common directory.
- Worktree configuration is `spor/config.json` in the worktree Git directory when that path is separate from the Git common directory.

`verify.command` is the program and its arguments.
`verify.mode` is `commit`, `patch`, or `change`.

```json
{
  "verify": {
    "command": ["go", "test", "./..."],
    "mode": "change"
  }
}
```
