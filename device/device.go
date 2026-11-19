// Package device finds, on the running device, the profile that describes it
// and the partitions that profile names.
package device

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	devices "github.com/singularityos-lab/sinty-devices"
	"github.com/singularityos-lab/sinty-devices/profile"
)

// Paths are where the running system exposes what the lookups read.
type Paths struct {
	Compatible string
	SysBlock   string
	Dev        string
}

// System is where Linux exposes them.
var System = Paths{Compatible: "/proc/device-tree/compatible", SysBlock: "/sys/class/block", Dev: "/dev"}

// ErrUnknown is returned on a device no profile describes, such as a PC
// without a device tree.
var ErrUnknown = errors.New("no profile describes this device")

// Profile is the catalog profile whose device tree compatible strings the
// running device lists, the most specific first.
func (p Paths) Profile() (*profile.Profile, error) {
	b, err := os.ReadFile(p.Compatible)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUnknown
		}
		return nil, err
	}
	profiles, err := devices.Profiles()
	if err != nil {
		return nil, err
	}
	for _, c := range strings.Split(strings.TrimRight(string(b), "\x00"), "\x00") {
		for _, pr := range profiles {
			for _, m := range pr.Match.Compatible {
				if m == c {
					return pr, nil
				}
			}
		}
	}
	return nil, ErrUnknown
}

// Partition is the block device of the partition named name.
func (p Paths) Partition(name string) (string, error) {
	entries, err := os.ReadDir(p.SysBlock)
	if err != nil {
		return "", err
	}
	want := []byte("PARTNAME=" + name + "\n")
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(p.SysBlock, e.Name(), "uevent"))
		if err != nil {
			continue
		}
		if bytes.HasPrefix(b, want) || bytes.Contains(b, append([]byte("\n"), want...)) {
			return filepath.Join(p.Dev, e.Name()), nil
		}
	}
	return "", fmt.Errorf("no %s partition", name)
}
