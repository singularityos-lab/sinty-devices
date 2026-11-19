// sinty-vendor-firmware installs the firmware a device's profile takes from
// the Android partition still on the device, such as a Pixel's vendor
// partition inside super: the owner's own copy, matched to the phone, read
// through a read-only mapping and never written to.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/mirkobrombin/atomloops/boot"
	"github.com/singularityos-lab/sinty-devices/android"
	"github.com/singularityos-lab/sinty-devices/device"
	"github.com/singularityos-lab/sinty-devices/profile"
)

const (
	mapping    = "sinty-vendor"
	stampName  = ".vendor-build"
	buildProp  = "build.prop"
	stampField = "ro.vendor.build.fingerprint="
)

var slotSuffix = regexp.MustCompile(`(?m)^androidboot\.slot_suffix = "_([ab])"$`)

func main() {
	out := flag.String("out", "/var/lib/sinty/firmware", "directory the firmware is installed to")
	sysBlock := flag.String("sys-block", "/sys/class/block", "block device directory")
	compatPath := flag.String("compatible", "/proc/device-tree/compatible", "device tree compatible")
	bootconfig := flag.String("bootconfig", "/proc/bootconfig", "boot configuration with the booted slot")
	flag.Parse()
	if err := run(*out, *sysBlock, *compatPath, *bootconfig); err != nil {
		fmt.Fprintln(os.Stderr, "sinty-vendor-firmware:", err)
		os.Exit(1)
	}
}

func run(out, sysBlock, compatPath, bootconfig string) error {
	dev := device.Paths{Compatible: compatPath, SysBlock: sysBlock, Dev: "/dev"}
	p, err := dev.Profile()
	if errors.Is(err, device.ErrUnknown) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.Firmware == nil || p.Firmware.Extract == nil {
		return nil
	}
	x := p.Firmware.Extract
	super, err := dev.Partition("super")
	if err != nil {
		return err
	}
	slot, err := bootedSlot(bootconfig)
	if err != nil {
		return err
	}
	disk, err := os.Open(super)
	if err != nil {
		return err
	}
	parts, err := android.ReadSuper(disk, slot)
	disk.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", super, err)
	}
	name := x.Partition + "_" + string(rune('a'+slot))
	part, ok := android.Find(parts, name)
	if !ok || part.Size() == 0 {
		return fmt.Errorf("super holds no %s", name)
	}
	segs := make([]boot.Segment, len(part.Extents))
	for i, e := range part.Extents {
		segs[i] = boot.Segment{Dev: super, Start: e.Start, Sectors: e.Sectors}
	}
	boot.CloseMapping(mapping)
	node, err := boot.OpenLinear(mapping, segs)
	if err != nil {
		return err
	}
	defer boot.CloseMapping(mapping)
	mnt, err := os.MkdirTemp("/run", "sinty-vendor-")
	if err != nil {
		return err
	}
	defer os.Remove(mnt)
	if err := mountReadOnly(node, mnt); err != nil {
		return fmt.Errorf("mount %s: %w", name, err)
	}
	defer syscall.Unmount(mnt, 0)

	stamp := fingerprint(filepath.Join(mnt, buildProp))
	if stamp != "" {
		if have, err := os.ReadFile(filepath.Join(out, stampName)); err == nil && string(have) == stamp+"\n" {
			return nil
		}
	}
	n, err := install(mnt, out, x.Files, stamp)
	if err != nil {
		return err
	}
	fmt.Printf("sinty-vendor-firmware: %d files from %s (%s) in %s\n", n, name, stamp, out)
	return nil
}

func bootedSlot(bootconfig string) (int, error) {
	b, err := os.ReadFile(bootconfig)
	if err != nil {
		return 0, err
	}
	m := slotSuffix.FindSubmatch(b)
	if m == nil {
		return 0, errors.New("the bootloader names no slot")
	}
	return int(m[1][0] - 'a'), nil
}

func mountReadOnly(dev, mnt string) error {
	flags := uintptr(syscall.MS_RDONLY | syscall.MS_NODEV | syscall.MS_NOSUID | syscall.MS_NOEXEC)
	err := syscall.Mount(dev, mnt, "ext4", flags, "noload")
	if err == nil {
		return nil
	}
	if err2 := syscall.Mount(dev, mnt, "erofs", flags, ""); err2 == nil {
		return nil
	}
	return err
}

func fingerprint(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if v, ok := strings.CutPrefix(s.Text(), stampField); ok {
			return v
		}
	}
	return ""
}

// install copies the files into a fresh directory next to out and puts it in
// place of out, so a reader sees either the old set or the whole new one.
func install(src, out string, files []profile.FileCopy, stamp string) (int, error) {
	next := out + ".new"
	os.RemoveAll(next)
	if err := os.MkdirAll(next, 0o755); err != nil {
		return 0, err
	}
	for _, f := range files {
		if err := copyFile(filepath.Join(src, f.From), filepath.Join(next, f.To)); err != nil {
			os.RemoveAll(next)
			return 0, err
		}
	}
	if err := os.WriteFile(filepath.Join(next, stampName), []byte(stamp+"\n"), 0o644); err != nil {
		os.RemoveAll(next)
		return 0, err
	}
	old := out + ".old"
	os.RemoveAll(old)
	if err := os.Rename(out, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if err := os.Rename(next, out); err != nil {
		return 0, err
	}
	os.RemoveAll(old)
	return len(files), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	o, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(o, in); err != nil {
		o.Close()
		return err
	}
	if err := o.Sync(); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}
