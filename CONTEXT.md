# Spor

Spor is the set of refactor commits taken from staged changes, together with the record of whether they verified.

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
The identity that decides whether a commit, patch, or change is already verified.
*Avoid*: Level

**Verification**:
The recorded result of checking a commit under a mode.
*Avoid*: Review
