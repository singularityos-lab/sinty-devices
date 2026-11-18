package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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

type Profile struct {
	Schema          int       `json:"schema"`
	ID              string    `json:"id"`
	Vendor          string    `json:"vendor"`
	Model           string    `json:"model"`
	Arch            string    `json:"arch"`
	MinFlashVersion string    `json:"min_flash_version"`
	Match           Match     `json:"match"`
	Install         Install   `json:"install"`
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
				bad("partition %q has unknown source %q", part.Name, part.Source)
			}
			if part.Name == "" {
				bad("partition without a name")
			}
		}
	default:
		bad("install method %q is unknown", p.Install.Method)
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
