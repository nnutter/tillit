# Local rename verifier

`Verify` compares two complete Go source files without reading the filesystem or importing packages.
It verifies only a rename of one explicitly declared, referenced local variable in a plain function, with every token and identifier binding otherwise preserved.
Whitespace is ignored, but comments and literal tokens must remain identical.
Both files must parse and type-check under Go 1.27 with no importer.

The accepted syntax is intentionally smaller than Go: top-level declarations must be non-generic functions without receivers or named results; function bodies support local variable declarations, assignments, returns, conditionals, simple expressions, and blocks.
Imports, directives, carriage returns, calls, selectors, composite types, generics, closures, and other syntax outside this subset are bypassed.
Invalid or untypecheckable input is also bypassed; a supported pair that fails the proof condition is rejected.
In particular, the verifier does not analyze package merges or other files, so its input must be self-contained.

A verified result assumes local names are semantically irrelevant and excludes observation of debug metadata and source positions.
It does not establish equivalence under tools or environments that observe those details, nor does it claim to verify other refactorings.
