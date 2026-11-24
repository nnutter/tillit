# Tillit

Tillit checks the existing Git commits on a feature branch.

## Language

**Commit**:
A Git commit on the feature branch, identified by its commit hash.
*Avoid*: Revision

**Validation**:
Tillit's recorded result of running a configured operational check on a commit.
Passing tests can provide empirical confidence that a refactor preserves behavior, but a passing check is not semantic verification.

**Verification**:
Conditional formal confidence that a refactor preserves behavior because checked language-semantic conditions imply behavior preservation under stated assumptions.
The current verifier only establishes this for a one-parent commit modifying one existing singleton, import-free Go file: an explicit local variable and its references are renamed while other tokens and bindings remain unchanged.
It assumes local names have no runtime significance and excludes debug metadata and source position observation.
Unsupported and rejected changes are not verified, and validation remains separate.
*Avoid*: Review
