# Spor

Spor tracks your changes, turns them into a coherent Git history, verifies every commit, and automatically makes fixup commits, as needed.

Spor will work in an adjacent branch/worktree to verify and fix-up commits, when you're ready, it will rebase those fixup commits back into your original branch.

Spor will analyze the staged changes to detect and extract refactor commits.

Spor's operational verification runs its configured check and records the outcome.
A successful operational verification does not by itself claim formal verification of behavior preservation.

## Refactor confidence

The Language section of `CONTEXT.md` defines validation and verification as different grounds for confidence in a refactor.
Validation is empirical confidence from existing tests still passing.
Verification is conditional formal confidence from checked language-semantic conditions that imply behavior preservation under stated assumptions.

For example, a verified variable rename requires the renamed references to retain their bindings to the renamed declaration and untouched references to retain their original bindings.
It also requires checking that no conflicts or capture change name resolution.
These checks must cover the relevant code and build configurations, and the stated assumptions must support the behavior-preservation claim.

A tool such as `gopls` is a candidate for performing or assisting with these checks, not automatic proof.
A `Verified` claim depends on the conditions actually checked, the tool's guarantees and version, the scope and build configurations covered, and the assumptions made.
Compilation or static acceptance alone is insufficient.
Tests complement verification and remain required by the project workflow.

Commit subjects can distinguish the evidence behind a refactor:

- `Refactor: Verified variable rename, foo`
- `Refactor: Validated extract function, foo()`

These are illustrative subjects, not claims that either refactor has been performed or verified.

## Example Workflow

In this workflow, verification and its success or failure refer to Spor's operational check, not necessarily formal verification.

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
- `spor daemon` - Runs the Spor daemon in the current Git worktree.
- `spor status` - Displays the status of the Spor daemon and any pending changes.
- `spor add` - Queues the staged changes for the daemon to process.
  `spor stage` is an alias for `spor add`.
- `spor reset` - Undoes the latest `spor add`.
- `spor log` - Shows added commits that are not yet on the user's branch with verification status.
- `spor rebase` - Queues integration of verified Spor commits into the current branch.
  `spor status` reports the outcome.
  The branch must be attached, and the worktree and index must be clean, with no Git operation in progress.
  Spor prepares the verified prefix and the remaining pending commits together.
  A conflict in either part can block the whole request without changing the user's branch or Spor history.
  After a partial integration, `spor add` is refused until the remaining Spor commits are integrated.
- `spor verify` - Queues a verification run. `--mode` selects the mode for that run.
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
- The daemon verifies unverified commits from the oldest forward and stops at the first failure.
- `spor verify` queues a check again, and `--mode` selects the mode for that run.
- `spor status` shows the verification outcome.

### 0.3.0

- Spor automatically makes fixup commits for commits that fail verification.
- The daemon owns the spor worktree and runs the Smith and Auditor.
- `spor add` and `spor verify` queue work for the daemon instead of running it in process.

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

`auditor.verify.command` is the program and its arguments.
`auditor.verify.mode` is `commit`, `patch`, or `change`.
These operational configuration names and Spor's verification statuses do not themselves imply formal verification.
`auditor.fixup.model.provider` and `auditor.fixup.model.version` select the `pi` agent.
`auditor.fixup.model.effort` is a `pi` thinking level such as `low`.
`mender.conflict.model.provider`, `mender.conflict.model.version`, and `mender.conflict.model.effort` configure `pi` for resolving rebase conflicts.
The provider and version are required only when a conflict occurs, and effort defaults to `low`.
Spor prepares integration in an isolated checkout and checks the branch and worktree again before publication.
External Git commands can still race with publication, so avoid changing the branch or worktree while a rebase request runs.

The following `go test` command supplies validation when existing tests pass, not verification in the formal sense defined above.

```json
{
  "auditor": {
    "verify": {
      "command": ["go", "test", "./..."],
      "mode": "change"
    },
    "fixup": {
      "model": {
        "provider": "openai",
        "version": "gpt-5",
        "effort": "low"
      }
    }
  }
}
```
