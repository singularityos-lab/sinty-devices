# Sinty devices

The device catalog for Sinty OS: one JSON profile per device model, telling the
installer and `sinty-flash` how to put Sinty on it without touching the firmware
the device shipped with.

`sinty-flash` reads the catalog from this repository at run time, so a new profile
reaches users as soon as it is merged, with no tool update. The Sinty installer
embeds the catalog at the version its image was built with, since it runs before
the device has a network.

## Layout

| Path | Purpose |
|---|---|
| `devices/<vendor>/<model>.json` | one profile per device model |
| `index.json` | the list tools fetch first, with the digest of every profile |
| `schema/profile.schema.json` | the profile format, for editors |
| `profile/` | the Go package that parses and validates profiles, shared with the tools |
| `devices.go` | the catalog embedded in Go, for the Sinty installer that runs offline |
| `cmd/validate` | the check CI runs on every pull request |

## Profile

```json
{
  "schema": 1,
  "id": "arduino/uno-q",
  "vendor": "Arduino",
  "model": "UNO Q",
  "arch": "aarch64",
  "min_flash_version": "0.1.0",
  "match": {
    "compatible": ["arduino,imola"],
    "usb": [{ "mode": "edl", "vid": "05c6", "pid": "9008" }]
  },
  "install": {
    "method": "installer-stick",
    "esp": "efi",
    "replace": ["rootfs", "userdata"]
  },
  "firmware": { "preserve": true, "restore": "https://..." }
}
```

- `id` is `<vendor>/<model>` and must match the file path.
- `min_flash_version` is the oldest `sinty-flash` that handles this profile
  correctly. When a device needs a fix in the tool, raise it in the same pull
  request that relies on the fix: older tools then refuse the device and ask the
  user to update instead of flashing it wrongly.
- `match` identifies the device: the device tree `compatible` strings seen by the
  installer, and the USB ids it shows in each mode (`edl`, `fastboot`, `ums`,
  `normal`) seen by `sinty-flash`.
- `install.method` is how Sinty gets on the device:
  - `installer-stick`: the device boots the Sinty installer from a USB stick.
    `esp` names the partition kept as the EFI system partition and `replace`
    lists the only partitions the installer may reformat. Every other partition
    is left as shipped.
  - `fastboot`: `sinty-flash` writes the listed `partitions` over fastboot, each
    from one part of the Sinty image (`esp`, `root`, `hash`, `data`). `unlock`
    names the bootloader unlock command when the device needs one.
- `firmware.restore` links the vendor's own way back to the original system.

## Adding a device

1. Add `devices/<vendor>/<model>.json`.
2. Run `go run ./cmd/validate -write` to refresh `index.json`.
3. Run `go test ./...` and `go run ./cmd/validate`, then open a pull request.

CI runs the same checks on every pull request.

## License

GPL-3.0-or-later.
