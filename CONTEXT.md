# Spor

Spor extracts refactor commits from tracked working changes and records whether they passed an operational check.

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
Spor's recorded result of running a configured operational check on a commit under a mode.
Passing tests can provide empirical confidence that a refactor preserves behavior, but a passing check is not semantic verification.

**Verification**:
Conditional formal confidence that a refactor preserves behavior because checked language-semantic conditions imply behavior preservation under stated assumptions.
Verification of existing feature-branch commits belongs to tillit, not Spor.
*Avoid*: Review
