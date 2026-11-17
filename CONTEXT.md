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
Spor's recorded result of running a configured operational check on a commit under a mode.
Passing tests can provide empirical confidence that a refactor preserves behavior, but a passing check is not semantic verification.

**Verification**:
Conditional formal confidence that a refactor preserves behavior because checked language-semantic conditions imply behavior preservation under stated assumptions.
The current verifier only establishes this for a one-parent commit modifying one existing singleton, import-free Go file: an explicit local variable and its references are renamed while other tokens and bindings remain unchanged.
It assumes local names have no runtime significance and excludes debug metadata and source position observation.
Unsupported and rejected changes are not verified, and validation remains separate.
*Avoid*: Review
