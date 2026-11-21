package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/singularityos-lab/sinty-devices/profile"
)

func vendorTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	for name, body := range map[string]string{
		"firmware/a.bin":   "aaa",
		"apex/camera.apex": "hal",
	} {
		p := filepath.Join(src, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return src
}

func tool(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstallRunsGenerators(t *testing.T) {
	src := vendorTree(t)
	out := filepath.Join(t.TempDir(), "firmware")
	x := &profile.Extract{
		Partition: "vendor",
		Files:     []profile.FileCopy{{From: "firmware/a.bin", To: "a.bin"}},
		Generate: []profile.Generate{{
			Name:  "camera",
			Tool:  tool(t, `[ "$1" = --apex ] && [ -d "$TMPDIR" ] || exit 3; cat "$2" > "$4/sensor.bin"`),
			Input: "apex/camera.apex",
			To:    "omamobile/camera",
			Args:  []string{"--apex", "{input}", "-d", "{output}"},
		}},
	}
	n, err := install(src, out, x, "build 1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("installed %d files, want 2", n)
	}
	got, err := os.ReadFile(filepath.Join(out, "omamobile/camera/sensor.bin"))
	if err != nil || string(got) != "hal" {
		t.Fatalf("generated file = %q, %v", got, err)
	}
	stamp, _ := os.ReadFile(filepath.Join(out, stampName))
	if string(stamp) != "build 1\n" {
		t.Fatalf("stamp = %q", stamp)
	}
	left, _ := filepath.Glob(filepath.Join(out, ".generate-*"))
	if len(left) != 0 {
		t.Fatalf("scratch left behind: %v", left)
	}
}

func TestFailedGeneratorKeepsFirmwareAndRetries(t *testing.T) {
	src := vendorTree(t)
	out := filepath.Join(t.TempDir(), "firmware")
	x := &profile.Extract{
		Partition: "vendor",
		Files:     []profile.FileCopy{{From: "firmware/a.bin", To: "a.bin"}},
		Generate: []profile.Generate{{
			Name: "camera", Tool: tool(t, "echo half > \"$4/x.bin\"; exit 1"),
			Input: "apex/camera.apex", To: "omamobile/camera",
			Args: []string{"--apex", "{input}", "-d", "{output}"},
		}},
	}
	if _, err := install(src, out, x, "build 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "a.bin")); err != nil {
		t.Fatal("copied firmware lost to a failed generator")
	}
	if _, err := os.Stat(filepath.Join(out, "omamobile/camera")); err == nil {
		t.Fatal("a failed generator left its partial output")
	}
	stamp, _ := os.ReadFile(filepath.Join(out, stampName))
	if strings.TrimSpace(string(stamp)) == "build 1" {
		t.Fatal("a failed generator was stamped as done")
	}
}

func TestStampFollowsTheProfile(t *testing.T) {
	a := &profile.Extract{Partition: "vendor", Files: []profile.FileCopy{{From: "a", To: "a"}}}
	b := &profile.Extract{Partition: "vendor", Files: a.Files, Generate: []profile.Generate{{Name: "camera"}}}
	if stampFor("", a) != "" {
		t.Fatal("no vendor build must give no stamp")
	}
	if stampFor("v1", a) == stampFor("v1", b) {
		t.Fatal("adding a generator kept the old stamp")
	}
	if stampFor("v1", a) != stampFor("v1", a) || !strings.HasPrefix(stampFor("v1", a), "v1 ") {
		t.Fatal("stamp is not stable")
	}
}
