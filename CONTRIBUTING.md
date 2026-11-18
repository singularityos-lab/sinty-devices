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
