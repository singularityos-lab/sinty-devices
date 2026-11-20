package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/singularityos-lab/sinty-devices/device"
)

var testLease = lease{server: deviceAddr, client: hostAddr, mask: linkMask, time: time.Hour}

func packet(kind byte, extra ...byte) []byte {
	b := make([]byte, headerLen)
	b[0], b[1], b[2] = bootRequest, 1, 6
	copy(b[4:8], []byte{0xde, 0xad, 0xbe, 0xef})
	copy(b[28:34], []byte{2, 1, 2, 3, 4, 5})
	b = append(b, magicCookie...)
	b = append(b, optMsgType, 1, kind)
	b = append(b, extra...)
	return append(b, optEnd)
}

func TestDiscoverIsOfferedTheComputerAddress(t *testing.T) {
	out := reply(packet(dhcpDiscover), testLease)
	if out == nil {
		t.Fatal("no offer")
	}
	opts := options(out[headerLen+4:])
	if out[0] != bootReply || !net.IP(out[16:20]).Equal(hostAddr) || opts[optMsgType][0] != dhcpOffer {
		t.Fatalf("op %d yiaddr %v type %v", out[0], net.IP(out[16:20]), opts[optMsgType])
	}
	if !bytes.Equal(out[4:8], []byte{0xde, 0xad, 0xbe, 0xef}) || !bytes.Equal(out[28:34], []byte{2, 1, 2, 3, 4, 5}) {
		t.Fatal("transaction id or client address not echoed")
	}
	if !bytes.Equal(opts[optMask], linkMask) || !net.IP(opts[optServerID]).Equal(deviceAddr) {
		t.Fatalf("mask %v server %v", opts[optMask], opts[optServerID])
	}
	if len(out) < minPacketLen {
		t.Fatalf("reply is %d bytes", len(out))
	}
}

func TestTheLinkNeverOffersARouteOrNameServer(t *testing.T) {
	for _, kind := range []byte{dhcpDiscover, dhcpRequest} {
		opts := options(reply(packet(kind, optRequested, 4, 172, 16, 42, 2), testLease)[headerLen+4:])
		if _, ok := opts[3]; ok {
			t.Fatal("router offered")
		}
		if _, ok := opts[6]; ok {
			t.Fatal("name server offered")
		}
	}
}

func TestRequestForTheComputerAddressIsAcked(t *testing.T) {
	out := reply(packet(dhcpRequest, optRequested, 4, 172, 16, 42, 2, optServerID, 4, 172, 16, 42, 1), testLease)
	opts := options(out[headerLen+4:])
	if opts[optMsgType][0] != dhcpAck || !net.IP(out[16:20]).Equal(hostAddr) {
		t.Fatalf("type %v yiaddr %v", opts[optMsgType], net.IP(out[16:20]))
	}
	if secs := opts[optLeaseTime]; len(secs) != 4 || secs[2] != 0x0e || secs[3] != 0x10 {
		t.Fatalf("lease %v", secs)
	}
}

func TestRequestForAnotherAddressIsRefused(t *testing.T) {
	out := reply(packet(dhcpRequest, optRequested, 4, 192, 168, 1, 50), testLease)
	if options(out[headerLen+4:])[optMsgType][0] != dhcpNak || !net.IP(out[16:20]).Equal(net.IPv4zero) {
		t.Fatal("a foreign address was not refused")
	}
}

func TestRenewalByClientAddressIsAcked(t *testing.T) {
	p := packet(dhcpRequest)
	copy(p[12:16], hostAddr)
	if options(reply(p, testLease)[headerLen+4:])[optMsgType][0] != dhcpAck {
		t.Fatal("renewal refused")
	}
}

func TestPacketsThatNeedNoAnswer(t *testing.T) {
	other := packet(dhcpRequest, optRequested, 4, 172, 16, 42, 2, optServerID, 4, 10, 0, 0, 1)
	served := packet(dhcpDiscover)
	served[0] = bootReply
	for name, p := range map[string][]byte{
		"release":      packet(7),
		"other server": other,
		"a reply":      served,
		"short":        []byte{1, 2, 3},
		"no cookie":    make([]byte, 300),
	} {
		if reply(p, testLease) != nil {
			t.Errorf("%s was answered", name)
		}
	}
}

func TestOptionsStopsAtTruncation(t *testing.T) {
	opts := options([]byte{0, optMsgType, 1, 1, optServerID, 9, 1})
	if len(opts) != 1 || opts[optMsgType][0] != 1 {
		t.Fatalf("%v", opts)
	}
}

func onBoard(t *testing.T, udcs ...string) string {
	t.Helper()
	dir := t.TempDir()
	old, oldDev := sys, dev
	sys = paths{
		UDC:       filepath.Join(dir, "udc"),
		Configfs:  filepath.Join(dir, "config"),
		MachineID: filepath.Join(dir, "machine-id"),
		OptIn:     filepath.Join(dir, "enabled"),
	}
	dev = device.Paths{Compatible: filepath.Join(dir, "compatible"), SysBlock: dir, Dev: dir}
	t.Cleanup(func() { sys, dev = old, oldDev })
	os.WriteFile(sys.MachineID, []byte("0123456789abcdef\n"), 0o644)
	for _, u := range udcs {
		os.MkdirAll(filepath.Join(sys.UDC, u), 0o755)
	}
	return dir
}

func TestOffWhenTheOwnerHasNotEnabledTheBridge(t *testing.T) {
	onBoard(t, "11210000.dwc3")
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sys.Configfs, "usb_gadget", gadgetName)); err == nil {
		t.Fatal("a gadget was created with the bridge off")
	}
}

func TestNothingToDoWithoutAController(t *testing.T) {
	onBoard(t)
	os.WriteFile(sys.OptIn, nil, 0o644)
	if err := run(); err != nil {
		t.Fatal(err)
	}
}

func TestGadgetIsBuiltAndBound(t *testing.T) {
	onBoard(t)
	g := gadget{root: filepath.Join(sys.Configfs, "usb_gadget", gadgetName)}
	fn := filepath.Join(g.root, "functions", function)
	os.MkdirAll(fn, 0o755)
	os.WriteFile(filepath.Join(fn, "ifname"), []byte("usb0\n"), 0o644)

	id := identity()
	ifname, err := g.create("11210000.dwc3", id, "Sinty on Google Pixel 7 Pro")
	if err != nil {
		t.Fatal(err)
	}
	if ifname != "usb0" {
		t.Fatalf("interface %q", ifname)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Join(g.root, rel))
		return string(b)
	}
	if read("UDC") != "11210000.dwc3" || read("strings/0x409/product") != "Sinty on Google Pixel 7 Pro" {
		t.Fatalf("UDC %q product %q", read("UDC"), read("strings/0x409/product"))
	}
	if read("functions/"+function+"/host_addr") != id.hostMAC || read("strings/0x409/serialnumber") != id.serial {
		t.Fatal("identity not written")
	}
	if target, err := os.Readlink(filepath.Join(g.root, "configs/c.1", function)); err != nil || target != fn {
		t.Fatalf("function link %q %v", target, err)
	}
}

func TestUnnamedInterfaceIsAnError(t *testing.T) {
	onBoard(t)
	g := gadget{root: filepath.Join(sys.Configfs, "usb_gadget", gadgetName)}
	fn := filepath.Join(g.root, "functions", function)
	os.MkdirAll(fn, 0o755)
	os.WriteFile(filepath.Join(fn, "ifname"), []byte("(unnamed net_device)\n"), 0o644)
	if _, err := g.create("udc", identity(), "Sinty"); err == nil {
		t.Fatal("an unnamed interface was accepted")
	}
}

func TestIdentityIsStableAndLocallyAdministered(t *testing.T) {
	onBoard(t)
	a, b := identity(), identity()
	if a != b {
		t.Fatal("identity changed between calls")
	}
	if a.devMAC == a.hostMAC || a.devMAC[:3] != "02:" || a.hostMAC[:3] != "02:" {
		t.Fatalf("dev %s host %s", a.devMAC, a.hostMAC)
	}
	os.WriteFile(sys.MachineID, []byte("another\n"), 0o644)
	if identity() == a {
		t.Fatal("two machines share an identity")
	}
}

func TestProductNameFallsBackWithoutAProfile(t *testing.T) {
	onBoard(t)
	if productName() != "Sinty" {
		t.Fatal(productName())
	}
}
