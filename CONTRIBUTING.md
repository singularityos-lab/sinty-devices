# Contributing to sinty-devices

Thanks for your interest in contributing!

## Quick Start

```bash
git clone https://github.com/singularityos-lab/sinty-devices
cd sinty-devices
go test ./...
go run ./cmd/validate
```

## Guidelines

- One device per pull request. A profile decides which partitions of a device
  get reformatted, so a wrong one can leave a device unbootable: describe how you
  tested it on the real hardware.
- Never list a firmware or boot chain partition in `replace` or `partitions`.
- Raise `min_flash_version` whenever the profile depends on a newer `sinty-flash`.
- Refresh `index.json` with `go run ./cmd/validate -write` in the same commit.
- By submitting a contribution you agree to the [CLA](CLA.md).

## License

GPL-3.0-or-later.

## Device- and environment-specific code

Code that exists only because of a specific device, architecture, firmware or environment must be marked on the line above it:

```
DEVICE-FIX(<scope>): <what breaks without this>
```

- `<scope>` is one of: `arm64`, `x86_64`, `uno-q`, `jetson`, `msm`, `nvidia`, `intel`, `amd`, `no-tpm`, `vm`, `u-boot`, `uefi`. A new scope is added to this list in the same pull request that first uses it.
- The text states what fails on that device without the code, not what the code does.
- Use the comment syntax of the file's language.
- This covers driver names, hardware probes, architecture constants, firmware quirks, boot-chain differences and anything gated on a specific environment.
- A pull request that adds or changes such code without the marker is not merged.
- `git grep -n "DEVICE-FIX("` lists every marker in the repository.
