# Status

The specification has 16 contract documents in the `draft` state. The reference implementation in `src/` passes all conformance vectors. The null implementation fails, as it must.

## Done

- Specification and conformance: the vector format, the runner, a null implementation and the vectors of each contract document.
- Core: the reference implementation over SQLite, the typed references, the snapshots, the input locks, lookup, rendering and the producer adapter contract.
- Transfer and composition: export, import, device stores and composed contexts, with end-to-end acceptance vectors.
- Activation gate: gated collections, the `activation` kind, the activation checks of `publish` and the `publications` read.

## Next

- Lifecycle and discovery.
- Integrations with producers and consumers. An implementation that integrates with a host application uses the tech spec of that application (constitution principle 9).

GitHub issues own the current work. The counts in [README.md](../README.md) come from the checks.

Temporary line for issue 12.
