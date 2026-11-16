# Spor

Spor is the set of refactor commits taken from staged changes, together with the record of whether they passed Spor's operational checks.

## Language

**Commit**:
A spor commit, identified on its own.
*Avoid*: Revision

**Patch**:
The stable identity of a commit's diff.
*Avoid*: Diff

**Change**:
The Change-Id identity of a commit.
*Avoid*: Patch, commit

**Mode**:
The identity that decides whether a commit, patch, or change has already passed Spor's operational check.
*Avoid*: Level

**Validation**:
Empirical confidence that a refactor preserves behavior because existing tests still pass.

**Verification**:
Conditional formal confidence that a refactor preserves behavior because checked language-semantic conditions imply behavior preservation under stated assumptions.
*Avoid*: Review

**Operational verification**:
Spor's recorded result of running its configured check on a commit under a mode.
A successful result does not itself claim formal verification.
