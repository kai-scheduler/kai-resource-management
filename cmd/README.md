# Commands

Each executable belongs in its own `cmd/<name>/` directory. Command packages
should contain process wiring, configuration, and startup code; reusable
implementation belongs under `pkg/`.

All commands share the root Go module and root Makefile. Do not add nested
`go.mod` or Makefile files.
