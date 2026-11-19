package device

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeDevice(t *testing.T, compatible string) Paths {
	t.Helper()
	dir := t.TempDir()
	p := Paths{Compatible: filepath.Join(dir, "compatible"), SysBlock: filepath.Join(dir, "block"), Dev: "/dev"}
	if compatible != "" {
		os.WriteFile(p.Compatible, []byte(compatible), 0o644)
	}
	for name, uevent := range map[string]string{
		"sda":   "MAJOR=8\nDEVNAME=sda\nDEVTYPE=disk\n",
		"sda12": "MAJOR=8\nPARTN=12\nPARTNAME=boot_b\n",
		"sda13": "PARTNAME=super\nMAJOR=8\n",
		"sda14": "MAJOR=8\nPARTNAME=boot_bx\n",
	} {
		os.MkdirAll(filepath.Join(p.SysBlock, name), 0o755)
		os.WriteFile(filepath.Join(p.SysBlock, name, "uevent"), []byte(uevent), 0o644)
	}
	return p
}

func TestProfileFollowsTheDeviceTree(t *testing.T) {
	p := fakeDevice(t, "google,gs201-cheetah\x00google,gs201\x00")
	pr, err := p.Profile()
	if err != nil || pr.ID != "google/pixel-7-pro" {
		t.Fatalf("%v %v", pr, err)
	}
	if _, err := fakeDevice(t, "acme,unknown\x00").Profile(); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown board: %v", err)
	}
	if _, err := fakeDevice(t, "").Profile(); !errors.Is(err, ErrUnknown) {
		t.Fatalf("no device tree: %v", err)
	}
}

func TestPartitionMatchesTheWholeName(t *testing.T) {
	p := fakeDevice(t, "")
	for name, want := range map[string]string{"boot_b": "/dev/sda12", "super": "/dev/sda13"} {
		if got, err := p.Partition(name); err != nil || got != want {
			t.Fatalf("%s: %s %v", name, got, err)
		}
	}
	if _, err := p.Partition("boot"); err == nil {
		t.Fatal("a partial name matched")
	}
}
