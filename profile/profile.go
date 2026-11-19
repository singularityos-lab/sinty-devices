package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const Schema = 1

type USB struct {
	Mode string `json:"mode"`
	VID  string `json:"vid"`
	PID  string `json:"pid"`
}

type Match struct {
	Compatible []string `json:"compatible,omitempty"`
	USB        []USB    `json:"usb,omitempty"`
}

type Partition struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type Install struct {
	Method     string      `json:"method"`
	ESP        string      `json:"esp,omitempty"`
	Replace    []string    `json:"replace,omitempty"`
	Partitions []Partition `json:"partitions,omitempty"`
	Unlock     string      `json:"unlock,omitempty"`
}

type Firmware struct {
	Preserve bool   `json:"preserve,omitempty"`
	Restore  string `json:"restore,omitempty"`
}

type SocID struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type DTBO struct {
	BoardID  string `json:"board_id"`
	BoardRev string `json:"board_rev"`
	PageSize int    `json:"page_size"`
}

type AVBFooter struct {
	Partition string `json:"partition"`
	Size      string `json:"size"`
}

type AVB struct {
	RollbackIndex uint64      `json:"rollback_index"`
	SecurityPatch string      `json:"security_patch"`
	Footers       []AVBFooter `json:"footers"`
}

// Boot says how to pack the kernel, device tree and initramfs for a
// bootloader that does not read them from a filesystem.
type Boot struct {
	Format         string   `json:"format"`
	Images         []string `json:"images"`
	DTB            string   `json:"dtb"`
	SocIDs         []SocID  `json:"soc_ids,omitempty"`
	Bootargs       string   `json:"bootargs,omitempty"`
	VendorCmdline  string   `json:"vendor_cmdline,omitempty"`
	VendorPageSize int      `json:"vendor_page_size"`
	OSVersion      string   `json:"os_version,omitempty"`
	OSPatchLevel   string   `json:"os_patch_level,omitempty"`
	DTBO           *DTBO    `json:"dtbo,omitempty"`
	AVB            *AVB     `json:"avb,omitempty"`
}

var BootImages = []string{"boot", "init_boot", "vendor_boot", "vendor_kernel_boot", "dtbo"}

func ParseHex32(s string) (uint32, error) {
	if !strings.HasPrefix(s, "0x") {
		return 0, fmt.Errorf("%q is not 0x-prefixed hex", s)
	}
	v, err := strconv.ParseUint(s[2:], 16, 32)
	return uint32(v), err
}

func (b *Boot) validate(bad func(string, ...any)) {
	if b.Format != "android-v4" {
		bad("boot format %q is unknown", b.Format)
		return
	}
	has := map[string]bool{}
	for _, i := range b.Images {
		if !slices.Contains(BootImages, i) {
			bad("boot image %q is unknown", i)
		}
		has[i] = true
	}
	if !has["boot"] || !has["vendor_boot"] {
		bad("android-v4 needs at least boot and vendor_boot")
	}
	if b.DTB == "" || strings.HasPrefix(b.DTB, "/") || strings.Contains(b.DTB, "..") {
		bad("boot dtb %q is not a path inside the kernel's dts directory", b.DTB)
	}
	for _, s := range b.SocIDs {
		if _, err := ParseHex32(s.ID); err != nil || s.Name == "" {
			bad("soc id %q: %v", s.Name, err)
		}
	}
	switch b.VendorPageSize {
	case 2048, 4096, 16384:
	default:
		bad("vendor_page_size %d is not 2048, 4096 or 16384", b.VendorPageSize)
	}
	if has["dtbo"] != (b.DTBO != nil) {
		bad("dtbo settings and the dtbo image go together")
	}
	if b.DTBO != nil {
		if _, err := ParseHex32(b.DTBO.BoardID); err != nil {
			bad("dtbo board_id: %v", err)
		}
		if _, err := ParseHex32(b.DTBO.BoardRev); err != nil {
			bad("dtbo board_rev: %v", err)
		}
		if b.DTBO.PageSize <= 0 {
			bad("dtbo page_size must be positive")
		}
	}
	if b.AVB != nil {
		for _, f := range b.AVB.Footers {
			if !has[f.Partition] {
				bad("avb footer for %q, which is not a boot image", f.Partition)
			}
			if _, err := ParseHex32(f.Size); err != nil {
				bad("avb footer size for %q: %v", f.Partition, err)
			}
		}
	}
}

type Profile struct {
	Schema          int       `json:"schema"`
	ID              string    `json:"id"`
	Vendor          string    `json:"vendor"`
	Model           string    `json:"model"`
	Arch            string    `json:"arch"`
	MinFlashVersion string    `json:"min_flash_version"`
	Match           Match     `json:"match"`
	Install         Install   `json:"install"`
	Boot            *Boot     `json:"boot,omitempty"`
	Firmware        *Firmware `json:"firmware,omitempty"`
	Notes           []string  `json:"notes,omitempty"`
}

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)
	hexPattern = regexp.MustCompile(`^[0-9a-f]{4}$`)
)

func Parse(data []byte) (*Profile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the profile")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Profile) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if p.Schema != Schema {
		bad("schema is %d, this reader knows %d", p.Schema, Schema)
	}
	if !idPattern.MatchString(p.ID) {
		bad("id %q is not vendor/model in lowercase", p.ID)
	}
	if strings.TrimSpace(p.Vendor) == "" || strings.TrimSpace(p.Model) == "" {
		bad("vendor and model are required")
	}
	if p.Arch != "x86_64" && p.Arch != "aarch64" {
		bad("arch %q is not x86_64 or aarch64", p.Arch)
	}
	if _, err := ParseVersion(p.MinFlashVersion); err != nil {
		bad("min_flash_version: %v", err)
	}
	if len(p.Match.Compatible) == 0 && len(p.Match.USB) == 0 {
		bad("match needs a device tree compatible or a USB id")
	}
	for _, c := range p.Match.Compatible {
		if strings.TrimSpace(c) == "" {
			bad("empty compatible string")
		}
	}
	for _, u := range p.Match.USB {
		switch u.Mode {
		case "edl", "fastboot", "ums", "normal":
		default:
			bad("usb mode %q is unknown", u.Mode)
		}
		if !hexPattern.MatchString(u.VID) || !hexPattern.MatchString(u.PID) {
			bad("usb id %s:%s is not four lowercase hex digits each", u.VID, u.PID)
		}
	}
	switch p.Install.Method {
	case "installer-stick":
		if p.Install.ESP == "" || len(p.Install.Replace) == 0 {
			bad("installer-stick needs esp and replace")
		}
		if len(p.Install.Partitions) > 0 {
			bad("installer-stick does not take partitions")
		}
	case "fastboot":
		if len(p.Install.Partitions) == 0 {
			bad("fastboot needs partitions")
		}
		for _, part := range p.Install.Partitions {
			switch part.Source {
			case "esp", "root", "hash", "data":
			default:
				if slices.Contains(BootImages, part.Source) {
					if p.Boot == nil || !slices.Contains(p.Boot.Images, part.Source) {
						bad("partition %q takes the %s image, which the boot section does not build", part.Name, part.Source)
					}
					continue
				}
				bad("partition %q has unknown source %q", part.Name, part.Source)
			}
			if part.Name == "" {
				bad("partition without a name")
			}
		}
	default:
		bad("install method %q is unknown", p.Install.Method)
	}
	if p.Boot != nil {
		p.Boot.validate(bad)
	}
	if p.Firmware != nil && p.Firmware.Restore != "" && !strings.HasPrefix(p.Firmware.Restore, "https://") {
		bad("firmware restore link must be https")
	}
	return errors.Join(errs...)
}

func (p *Profile) MatchesUSB(vid, pid string) (string, bool) {
	for _, u := range p.Match.USB {
		if strings.EqualFold(u.VID, vid) && strings.EqualFold(u.PID, pid) {
			return u.Mode, true
		}
	}
	return "", false
}

func (p *Profile) MatchesCompatible(compat []string) bool {
	for _, want := range p.Match.Compatible {
		for _, c := range compat {
			if c == want {
				return true
			}
		}
	}
	return false
}

type Version [3]int

func ParseVersion(s string) (Version, error) {
	var v Version
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("%q is not major.minor.patch", s)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || part != strconv.Itoa(n) {
			return v, fmt.Errorf("%q is not major.minor.patch", s)
		}
		v[i] = n
	}
	return v, nil
}

func (v Version) Less(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] < o[i]
		}
	}
	return false
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

type Entry struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	Vendor          string `json:"vendor"`
	Model           string `json:"model"`
	MinFlashVersion string `json:"min_flash_version"`
	SHA256          string `json:"sha256"`
}

type Index struct {
	Schema  int     `json:"schema"`
	Devices []Entry `json:"devices"`
}

func ParseIndex(data []byte) (*Index, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var ix Index
	if err := dec.Decode(&ix); err != nil {
		return nil, err
	}
	if ix.Schema != Schema {
		return nil, fmt.Errorf("index schema is %d, this reader knows %d", ix.Schema, Schema)
	}
	return &ix, nil
}

func (ix *Index) Find(id string) (Entry, bool) {
	for _, e := range ix.Devices {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
