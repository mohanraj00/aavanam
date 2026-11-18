# Activation records from entelos v0.5.0

This directory holds the program and the files that make the activation records for the activation seam test and the library example.

## How to make the records again

The default command expects an entelos checkout at the pinned commit in the folder `entelos` at the repository root, as in CI.

Run this command from this directory:

```sh
go run .
```

If entelos is in another location, update the replace directive. Do not commit the changed `go.mod`:

```sh
go mod edit -replace github.com/mohanraj00/entelos/src/lifecycle-core=/path/to/entelos/src/lifecycle-core
go run .
```

## Determinism

The program uses a fixed public test seed from `seed.txt` for Ed25519 signing.
If `seed.txt` is missing, unreadable or malformed, the program stops with exit status 1 before it writes a file. It has no built-in seed.
The entelos Host writes a `submitted_at` timestamp on each snapshot. The program installs a fixed clock with value `2026-01-01T00:00:00Z`.
The entelos Host also writes its build identity into its reports. By default, this identity holds the Go version and the output of `git --version`. Thus the default value changes with the toolchain.
The program sets the Host build identity to a fixed test value: the text `sha256:` and 64 zeros. The records are test data. The fixed build value is not the identity of a real build.
Because the seed, the clock, the build value and the inputs are fixed, repeated runs make the same record bytes, references and signatures with each toolchain.

## Files

- `main.go`: the Go program that runs the Host operations and writes the records.
- `go.mod`: the Go module definition with the local replace directive.
- `seed.txt`: the fixed Ed25519 test seed. Marked as public test data only.
- `keyset.json`: the public key set (`entelos.keyset/1`) of the test signer.
- `manifest_first.json`: the content manifest for notice update 1.
- `manifest_second.json`: the content manifest for notice update 2.
- `first_record.json`: the activation record bytes for notice update 1.
- `first_signed.json`: the signed activation record for notice update 1.
- `second_record.json`: the activation record bytes for notice update 2.
- `second_signed.json`: the signed activation record for notice update 2.
- `event_signed.json`: a signed event record used to test refusal of wrong record kinds.
