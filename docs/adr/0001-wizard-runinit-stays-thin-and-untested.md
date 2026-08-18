---
status: accepted
---

# `wizard.RunInit` stays thin and untested — no sequencing extraction, no prompter seam

`RunInit` (`internal/wizard/run.go`) is a straight-line function that chains four `huh` I/O interactions with two one-line abort checks; every other decision (defaults, prefill, overwrite confirmation, `.env` rendering, reachability checks) already lives in small, tested leaf functions in `wizard.go`, exactly as that file's package doc says it should. During the 2026-08-18 `/improve-codebase-architecture` review, we considered extracting `RunInit`'s sequencing into a pure `planWizardFlow` module, and separately considered introducing a `prompter` interface (mirroring the `sessionSource`/`sessionSink` seam adopted for `internal/sync/orchestrator.go`, see #27) so a scripted fake could drive the whole flow end-to-end in tests. We rejected both.

The deletion test doesn't support the `planWizardFlow` extraction: `RunInit`'s body is almost entirely I/O calls already reduced to their minimum, so pulling the sequencing out would mostly reproduce the same five steps as a data structure plus an interpreter, without concentrating any complexity that isn't already about as concentrated as it can be. The `prompter` seam would close a real gap — there's no test today covering the two abort paths or the overall sequence — but the risk it guards against (an ordering bug like write-before-confirm passing silently) is low-severity here: `WriteEnvFile` is the unconditional last line of a single top-to-bottom function, not spread across indirection, and `gcs-connector init` is a human-supervised interactive command where a wrong prompt is immediately visible when run, not a silent background failure.

## Reconsider if

`RunInit`'s flow gains real branching (more conditional steps, not just more fields), or `init` ever needs to run unattended/scripted rather than interactively — either would strengthen the case for the `prompter` seam.
