---
name: tillit
description: Retrieve Tillit validation feedback for failed branch commits and resolve fixable failures with commit-stack fixup commits.
---

# Address Tillit validation failures

Use this skill when Tillit reports failed validation on a feature branch or when asked to inspect and fix Tillit validation feedback.
Run commands from the feature worktree.

## Retrieve validation status and feedback

Inspect branch status (daemon state, validation, and verification per commit stack) with:

```sh
tillit status --json
```

Pull recorded validation failures, command output, and reproduction commands with:

```sh
tillit feedback --json
```

Use `tillit feedback` without `--json` for a human-readable failure summary, or `tillit status --simple` for plain-text status.
Both `tillit status --json` and `tillit status --simple` imply `--no-start` and do not start the daemon.

`tillit feedback --json` is read-only and returns recorded evidence without running checks:

- `version`: report schema version (`1`).
- `branch`, `tip`, `boundary`: feature branch name, `HEAD` commit ID, and base commit ID.
- `summary`: counts by state (`passed`, `failed`, `pending`, `running`, `error`).
- `commits`: ordered commit stacks from oldest to newest, each containing:
  - `commit`: full hash of the stack tip (the original commit plus any attached fixups).
  - `subject`: subject of the stack's original commit.
  - `validation`: current state (`passed`, `failed`, `pending`, `running`, `error`).
  - `error`: failure or configuration error message when present.
  - `evidence`: recorded `command`, `output`, `output_truncated`, and `failures` (`kind`, `package`, `test`, `output`, `reproductions`).

Tillit stops validating a branch on the first failing commit stack, so later stacks can remain `pending` until earlier failures pass.

## Decide whether the failure is resolvable

Before editing code, inspect the failing commit stack and its neighbors:

```sh
git log --oneline <boundary>..<tip>
git show <commit>
```

Evaluate whether the failing commit is supposed to pass validation on its own:

- **Resolvable at this commit**: the failure is a bug, typo, missing import, lint diagnostic, or broken test introduced by this commit stack that can be fixed cleanly within the commit's intended scope.
- **Unresolvable at this commit**: the commit is intentionally one part of a multi-commit sequence that cannot build or pass tests until a later commit lands, or the commit itself is partway through addressing a pre-existing failure.
  For example, if commits 1, 2, and 3 are all required for validation to succeed and are intentionally kept separate for reviewability, commits 1 and 2 cannot pass in isolation without merging them.

Do not squash or merge separate commits to force validation to pass unless the user explicitly asks.
Do not pull unrelated changes from later commits backward into an earlier commit just to make validation green.
When a failure is unresolvable without collapsing intentional commit boundaries, leave that commit unchanged, explain why it cannot pass on its own and which later commit resolves it, and ask the user how they want to proceed.

## Fix resolvable failures with commit-stack fixups

Address each resolvable failure with a native Git `fixup!` commit placed immediately after the failing stack tip.
Do not use `git commit --amend` to rewrite the target commit in place, and do not append a fixup for an earlier commit to the end of the branch without inserting it after its target stack.

Before rewriting history, check that the working tree is clean and record a backup ref under `refs/agents/` (not `refs/heads/`):

```sh
git update-ref refs/agents/tillit-fixup/backup HEAD
```

### Target stack is at HEAD

1. Make the minimal fix needed for the failing validation check.
1. Run the recorded validation command (or reproduction command from `evidence.failures[].reproductions`) to confirm the failure is resolved.
1. Stage the changes and create a fixup commit targeting the stack tip:

```sh
git commit --fixup="$target_tip"
```

### Target stack is earlier in the branch

1. Record the current branch name and check out the failing stack tip on a detached `HEAD`:

```sh
feature_branch=$(git branch --show-current)
git switch --detach "$target_tip"
```

1. Implement the fix and run the failing validation command at this commit to confirm it passes.
1. Stage the changes and create the fixup commit:

```sh
git commit --fixup="$target_tip"
```

1. Replay the remaining branch commits onto the new stack tip without autosquashing:

```sh
new_tip=$(git rev-parse HEAD)
git rebase --no-autosquash --no-update-refs --reapply-cherry-picks \
  --empty=keep --onto "$new_tip" "$target_tip" "$feature_branch"
```

Resolve any rebase conflicts without discarding unrelated descendant changes.
If conflict resolution is unsafe or unclear, abort the rebase (`git rebase --abort`), restore the branch from the backup ref if needed, and ask the user.
Keep original and `fixup!` commits separate unless the user explicitly requests squashing.

## Verify and report

1. Confirm the checkout is back on the feature branch with a clean working tree.
1. Run the repository's validation checks on the final branch tip.
1. Check `tillit status --json` or `tillit feedback --json` after the daemon re-validates the updated stacks.
1. Report each failing stack tip: either the fixup commit created and its verification result, or why the failure was left unaddressed because it cannot be resolved at that commit alone.
