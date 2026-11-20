// sinty-usb-link carries the Sinty Debug Bridge over the USB cable on devices
// whose USB controller can act as a device. When the owner turned the bridge on,
// it presents a USB network function to the computer on the other end of the
// cable, gives the device the first address of a two-host link and hands the
// computer the second one over DHCP, so `sdb pair 172.16.42.1` works with no
// setup on the computer. The link carries no route and no name server: the
// computer never sends its own traffic through the device.
package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/singularityos-lab/sinty-devices/device"
)

const (
	gadgetName = "sinty"
	function   = "ncm.usb0"
	optIn      = "/var/lib/sinty-sdb/enabled"
)

var (
	deviceAddr = net.IPv4(172, 16, 42, 1).To4()
	hostAddr   = net.IPv4(172, 16, 42, 2).To4()
	linkMask   = net.CIDRMask(30, 32)
)

type paths struct {
	UDC       string
	Configfs  string
	MachineID string
	OptIn     string
}

var (
	sys   = paths{UDC: "/sys/class/udc", Configfs: "/sys/kernel/config", MachineID: "/etc/machine-id", OptIn: optIn}
	dev   = device.System
	runIP = func(args ...string) error {
		out, err := exec.Command("ip", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sinty-usb-link:", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := os.Stat(sys.OptIn); err != nil {
		fmt.Println("sinty-usb-link: the debug bridge is off")
		return nil
	}
	udc, err := firstUDC()
	if err != nil {
		return err
	}
	if udc == "" {
		fmt.Println("sinty-usb-link: no USB device controller")
		return nil
	}
	if err := mountConfigfs(); err != nil {
		return err
	}
	g := gadget{root: filepath.Join(sys.Configfs, "usb_gadget", gadgetName)}
	g.remove()
	ifname, err := g.create(udc, identity(), productName())
	if err != nil {
		g.remove()
		return err
	}
	defer g.remove()
	if err := runIP("addr", "replace", (&net.IPNet{IP: deviceAddr, Mask: linkMask}).String(), "dev", ifname); err != nil {
		return err
	}
	if err := runIP("link", "set", ifname, "up"); err != nil {
		return err
	}
	srv, err := listenDHCP(ifname)
	if err != nil {
		return err
	}
	defer srv.Close()
	fmt.Printf("sinty-usb-link: %s on %s, device %s, computer %s\n", ifname, udc, deviceAddr, hostAddr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	errs := make(chan error, 1)
	go func() {
		errs <- srv.Serve(lease{server: deviceAddr, client: hostAddr, mask: linkMask, time: 24 * time.Hour})
	}()
	select {
	case <-stop:
		return nil
	case err := <-errs:
		return err
	}
}

func firstUDC() (string, error) {
	entries, err := os.ReadDir(sys.UDC)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	return entries[0].Name(), nil
}

func mountConfigfs() error {
	if _, err := os.Stat(filepath.Join(sys.Configfs, "usb_gadget")); err == nil {
		return nil
	}
	if err := syscall.Mount("configfs", sys.Configfs, "configfs", 0, ""); err != nil && !errors.Is(err, syscall.EBUSY) {
		return fmt.Errorf("mount configfs: %w", err)
	}
	if _, err := os.Stat(filepath.Join(sys.Configfs, "usb_gadget")); err != nil {
		return errors.New("the kernel has no USB gadget configfs")
	}
	return nil
}

type ids struct {
	serial  string
	devMAC  string
	hostMAC string
}

// productName is what the computer lists for the device: its model when a
// profile knows it.
func productName() string {
	if p, err := dev.Profile(); err == nil && p.Model != "" {
		return "Sinty on " + p.Vendor + " " + p.Model
	}
	return "Sinty"
}

// identity derives the serial number and both link addresses from the machine
// id, so the computer sees the same device, and keeps the same connection for
// it, across boots.
func identity() ids {
	seed, _ := os.ReadFile(sys.MachineID)
	sum := sha256.Sum256(append([]byte("sinty-usb-link\x00"), seed...))
	mac := func(tag byte) string {
		return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", tag, sum[0], sum[1], sum[2], sum[3])
	}
	return ids{serial: fmt.Sprintf("%x", sum[4:12]), devMAC: mac(0x5d), hostMAC: mac(0x5e)}
}

type gadget struct {
	root string
}

func (g gadget) write(rel, value string) error {
	return os.WriteFile(filepath.Join(g.root, rel), []byte(value), 0o644)
}

func (g gadget) create(udc string, id ids, product string) (string, error) {
	for _, dir := range []string{"strings/0x409", "configs/c.1/strings/0x409", "functions/" + function} {
		if err := os.MkdirAll(filepath.Join(g.root, dir), 0o755); err != nil {
			return "", err
		}
	}
	files := []struct{ rel, value string }{
		{"idVendor", "0x1d6b"},
		{"idProduct", "0x0104"},
		{"bcdUSB", "0x0200"},
		{"strings/0x409/manufacturer", "Sinty"},
		{"strings/0x409/product", product},
		{"strings/0x409/serialnumber", id.serial},
		{"configs/c.1/strings/0x409/configuration", "Sinty Debug Bridge"},
		{"configs/c.1/MaxPower", "500"},
		{"functions/" + function + "/dev_addr", id.devMAC},
		{"functions/" + function + "/host_addr", id.hostMAC},
	}
	for _, f := range files {
		if err := g.write(f.rel, f.value); err != nil {
			return "", err
		}
	}
	link := filepath.Join(g.root, "configs/c.1", function)
	if err := os.Symlink(filepath.Join(g.root, "functions", function), link); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := g.write("UDC", udc); err != nil {
		return "", fmt.Errorf("bind %s: %w", udc, err)
	}
	name, err := os.ReadFile(filepath.Join(g.root, "functions", function, "ifname"))
	if err != nil {
		return "", err
	}
	ifname := strings.TrimSpace(string(name))
	if ifname == "" || strings.Contains(ifname, "unnamed") {
		return "", errors.New("the network function got no interface")
	}
	return ifname, nil
}

// remove unbinds and dismantles the gadget in the reverse order configfs
// requires. A gadget that is not there is not an error.
func (g gadget) remove() {
	if _, err := os.Stat(g.root); err != nil {
		return
	}
	_ = g.write("UDC", "\n")
	_ = os.Remove(filepath.Join(g.root, "configs/c.1", function))
	_ = os.Remove(filepath.Join(g.root, "configs/c.1/strings/0x409"))
	_ = os.Remove(filepath.Join(g.root, "configs/c.1"))
	_ = os.Remove(filepath.Join(g.root, "functions", function))
	_ = os.Remove(filepath.Join(g.root, "strings/0x409"))
	_ = os.Remove(g.root)
}
