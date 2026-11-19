package profile

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

const unoQ = `{
  "schema": 1,
  "id": "arduino/uno-q",
  "vendor": "Arduino",
  "model": "UNO Q",
  "arch": "aarch64",
  "min_flash_version": "0.1.0",
  "match": {"compatible": ["arduino,imola"], "usb": [{"mode": "edl", "vid": "05c6", "pid": "9008"}]},
  "install": {"method": "installer-stick", "esp": "efi", "replace": ["rootfs", "userdata"]}
}`

func TestParseValid(t *testing.T) {
	p, err := Parse([]byte(unoQ))
	if err != nil {
		t.Fatal(err)
	}
	if mode, ok := p.MatchesUSB("05C6", "9008"); !ok || mode != "edl" {
		t.Fatalf("usb match = %q %v", mode, ok)
	}
	if !p.MatchesCompatible([]string{"qcom,qcm2290", "arduino,imola"}) {
		t.Fatal("compatible not matched")
	}
	if p.MatchesCompatible([]string{"qcom,qcm2290"}) {
		t.Fatal("matched a foreign compatible")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field":   strings.Replace(unoQ, `"arch"`, `"colour": "red", "arch"`, 1),
		"bad id":          strings.Replace(unoQ, `"arduino/uno-q"`, `"Arduino/UNO Q"`, 1),
		"bad version":     strings.Replace(unoQ, `"0.1.0"`, `"0.1"`, 1),
		"bad method":      strings.Replace(unoQ, `"installer-stick"`, `"dd"`, 1),
		"stick no esp":    strings.Replace(unoQ, `"esp": "efi", `, ``, 1),
		"upper usb":       strings.Replace(unoQ, `"05c6"`, `"05C6"`, 1),
		"newer schema":    strings.Replace(unoQ, `"schema": 1`, `"schema": 2`, 1),
		"fastboot empty":  strings.Replace(unoQ, `"method": "installer-stick", "esp": "efi", "replace": ["rootfs", "userdata"]`, `"method": "fastboot"`, 1),
		"trailing object": unoQ + `{}`,
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVersion(t *testing.T) {
	a, _ := ParseVersion("0.1.9")
	b, _ := ParseVersion("v0.2.0")
	if !a.Less(b) || b.Less(a) || a.Less(a) {
		t.Fatal("ordering")
	}
	for _, bad := range []string{"1.2", "1.2.x", "01.2.3", "-1.2.3", ""} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestBuildIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"devices/arduino/uno-q.json": {Data: []byte(unoQ)},
	}
	ix, err := BuildIndex(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Devices) != 1 || ix.Devices[0].SHA256 != Digest([]byte(unoQ)) {
		t.Fatalf("index = %+v", ix)
	}
	if _, ok := ix.Find("arduino/uno-q"); !ok {
		t.Fatal("not found")
	}
}

func TestBuildIndexRejectsMisplaced(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"wrong path": {"devices/arduino/uno.json": {Data: []byte(unoQ)}},
		"too deep":   {"devices/arduino/q/uno-q.json": {Data: []byte(unoQ)}},
		"top level":  {"devices/uno-q.json": {Data: []byte(unoQ)}},
	} {
		if _, err := BuildIndex(fsys); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPixelBootProfile(t *testing.T) {
	data, err := os.ReadFile("../devices/google/pixel-7-pro.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Boot == nil || p.Boot.DTBO == nil || len(p.Boot.AVB.Footers) != 2 {
		t.Fatalf("boot section not read: %+v", p.Boot)
	}
	broken := strings.Replace(string(data), `"partition": "init_boot"`, `"partition": "recovery"`, 1)
	if _, err := Parse([]byte(broken)); err == nil {
		t.Fatal("a footer for an image the profile does not build must fail")
	}
	noBoot := strings.Replace(string(data), `"source": "dtbo"`, `"source": "dtb"`, 1)
	if _, err := Parse([]byte(noBoot)); err == nil {
		t.Fatal("an unknown partition source must fail")
	}
}
