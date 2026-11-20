// sinty-android-boot keeps the boot partitions of a device that boots like an
// Android phone in step with the root it runs. Atom Loops runs it as a kernel
// hook: when a candidate is staged it writes the candidate's kernel, and on
// every boot it writes back the booted release's kernel when another one is in
// place, then restarts.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/singularityos-lab/sinty-bootimg/bootimg"
	"github.com/singularityos-lab/sinty-devices/device"
	"github.com/singularityos-lab/sinty-devices/profile"
)

const (
	blkROSet  = 0x125d
	blkFlsBuf = 0x1261
)

var (
	stampPath = "/var/lib/sinty/android-boot"
	dev       = device.System
	restart   = func() error { return exec.Command("reboot").Run() }
	installer = install
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sinty-android-boot staged|booted")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Getenv("ATOM_KERNELCACHE"), os.Getenv("ATOM_KERNELCACHE_HASH")); err != nil {
		fmt.Fprintln(os.Stderr, "sinty-android-boot:", err)
		os.Exit(1)
	}
}

func run(phase, kernelcache, hash string) error {
	if phase != "staged" && phase != "booted" {
		return fmt.Errorf("unknown phase %q", phase)
	}
	p, err := dev.Profile()
	if errors.Is(err, device.ErrUnknown) {
		return nil
	}
	if err != nil {
		return err
	}
	if p.Boot == nil {
		return nil
	}
	if kernelcache == "" || hash == "" {
		return errors.New("no kernelcache was handed over")
	}
	if have, err := os.ReadFile(stampPath); err == nil && strings.TrimSpace(string(have)) == hash {
		return nil
	}
	if err := installer(p, kernelcache); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(stampPath), 0o755); err != nil {
		return err
	}
	if err := writeSynced(stampPath, []byte(hash+"\n")); err != nil {
		return err
	}
	fmt.Printf("sinty-android-boot: %s kernel %s installed\n", phase, hash[:min(12, len(hash))])
	if phase == "booted" {
		return restart()
	}
	return nil
}

// install builds the boot images from the kernelcache, as sinty-flash does,
// and writes each one to the partition the profile gives it. Every image is
// built and every partition found before the first write.
func install(p *profile.Profile, kernelcache string) error {
	uki, err := os.ReadFile(kernelcache)
	if err != nil {
		return err
	}
	in := bootimg.Inputs{}
	if in.Kernel, in.Initramfs, in.DTB, err = bootimg.FromUKI(uki, p.Match.Compatible[0]); err != nil {
		return err
	}
	if p.Boot.AVB != nil {
		if in.Key, err = rsa.GenerateKey(rand.Reader, 4096); err != nil {
			return err
		}
	}
	imgs, err := bootimg.Android(p.Boot, in)
	if err != nil {
		return err
	}
	type write struct {
		node string
		img  []byte
	}
	var writes []write
	for _, part := range p.Install.Partitions {
		if !slices.Contains(profile.BootImages, part.Source) {
			continue
		}
		img, ok := imgs[part.Source]
		if !ok {
			return fmt.Errorf("the boot section builds no %s image", part.Source)
		}
		node, err := dev.Partition(part.Name)
		if err != nil {
			return err
		}
		writes = append(writes, write{node, img})
	}
	if len(writes) == 0 {
		return errors.New("the profile writes no boot partition")
	}
	for _, w := range writes {
		if err := writePartition(w.node, w.img); err != nil {
			return fmt.Errorf("%s: %w", w.node, err)
		}
	}
	return nil
}

// writePartition lifts the read-only flag the boot's disk guard set, writes
// img at the start of the partition, reads it back, and sets the flag again.
func writePartition(node string, img []byte) error {
	ro, err := os.Open(node)
	if err != nil {
		return err
	}
	err = setRO(ro, 0)
	ro.Close()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(node, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		setRO(f, 1)
		f.Close()
	}()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if int64(len(img)) > size {
		return fmt.Errorf("the image is %d bytes, the partition %d", len(img), size)
	}
	if _, err := f.WriteAt(img, 0); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ioctl(f, blkFlsBuf, 0)
	back := make([]byte, len(img))
	if _, err := f.ReadAt(back, 0); err != nil {
		return err
	}
	if !bytes.Equal(back, img) {
		return errors.New("the partition does not read back what was written")
	}
	return nil
}

func setRO(f *os.File, on int32) error {
	return ioctl(f, blkROSet, uintptr(unsafe.Pointer(&on)))
}

// ioctl runs a block device request, which a regular file standing in for a
// partition does not take.
func ioctl(f *os.File, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, arg); errno != 0 {
		if errno == syscall.ENOTTY {
			return nil
		}
		return errno
	}
	return nil
}

func writeSynced(path string, b []byte) error {
	tmp := path + ".new"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
