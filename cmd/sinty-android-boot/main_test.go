package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/singularityos-lab/sinty-devices/device"
	"github.com/singularityos-lab/sinty-devices/profile"
)

func onDevice(t *testing.T, compatible string) (installs *int, restarts *int) {
	t.Helper()
	dir := t.TempDir()
	if compatible != "" {
		os.WriteFile(filepath.Join(dir, "compatible"), []byte(compatible), 0o644)
	}
	oldDev, oldStamp, oldRestart, oldInstall := dev, stampPath, restart, installer
	dev = device.Paths{Compatible: filepath.Join(dir, "compatible"), SysBlock: dir, Dev: dir}
	stampPath = filepath.Join(dir, "state", "android-boot")
	var i, r int
	installer = func(*profile.Profile, string) error { i++; return nil }
	restart = func() error { r++; return nil }
	t.Cleanup(func() { dev, stampPath, restart, installer = oldDev, oldStamp, oldRestart, oldInstall })
	return &i, &r
}

func TestStagedWritesTheCandidateKernelOnce(t *testing.T) {
	installs, restarts := onDevice(t, "google,gs201-cheetah\x00")
	for i := 0; i < 2; i++ {
		if err := run("staged", "/k", "aaaa1111"); err != nil {
			t.Fatal(err)
		}
	}
	if *installs != 1 || *restarts != 0 {
		t.Fatalf("installs %d restarts %d", *installs, *restarts)
	}
}

func TestBootedRestoresAnotherKernelAndRestarts(t *testing.T) {
	installs, restarts := onDevice(t, "google,gs201-cheetah\x00")
	run("staged", "/k", "new")
	if err := run("booted", "/k", "new"); err != nil || *installs != 1 || *restarts != 0 {
		t.Fatalf("booted the staged kernel: %v installs %d restarts %d", err, *installs, *restarts)
	}
	if err := run("booted", "/k", "old"); err != nil || *installs != 2 || *restarts != 1 {
		t.Fatalf("booted after a failed trial: %v installs %d restarts %d", err, *installs, *restarts)
	}
	if b, _ := os.ReadFile(stampPath); string(b) != "old\n" {
		t.Fatalf("stamp %q", b)
	}
}

func TestDevicesWithoutABootSectionAreLeftAlone(t *testing.T) {
	for _, c := range []string{"", "arduino,imola\x00"} {
		installs, _ := onDevice(t, c)
		if err := run("staged", "/k", "x"); err != nil || *installs != 0 {
			t.Fatalf("%q: %v installs %d", c, err, *installs)
		}
	}
}

func TestWritePartitionReadsBack(t *testing.T) {
	part := filepath.Join(t.TempDir(), "boot_b")
	os.WriteFile(part, make([]byte, 4096), 0o644)
	img := bytes.Repeat([]byte("sinty"), 100)
	if err := writePartition(part, img); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(part)
	if !bytes.Equal(b[:len(img)], img) || len(b) != 4096 {
		t.Fatal("the partition does not hold the image")
	}
	if err := writePartition(part, make([]byte, 8192)); err == nil {
		t.Fatal("an image larger than the partition was written")
	}
}
