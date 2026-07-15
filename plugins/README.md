# plugins

Placeholder for future externally-compiled provider binaries. No loader
exists yet — this directory is intentionally empty pending a later phase of
the project. See [`docs/plugins.md`](../docs/plugins.md) for the design.

This is **not** where today's in-tree provider plugins live — those are Go
packages compiled into the `aud` binary under `internal/plugins/<provider>/`.
See [`docs/providers.md`](../docs/providers.md#plugin-package-convention).
