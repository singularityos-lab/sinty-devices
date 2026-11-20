package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

const (
	dhcpDiscover = 1
	dhcpOffer    = 2
	dhcpRequest  = 3
	dhcpAck      = 5
	dhcpNak      = 6
	dhcpInform   = 8

	optMask      = 1
	optRequested = 50
	optLeaseTime = 51
	optMsgType   = 53
	optServerID  = 54
	optEnd       = 255
	bootRequest  = 1
	bootReply    = 2
	headerLen    = 236
	minPacketLen = 300
)

var magicCookie = []byte{99, 130, 83, 99}

var kindName = map[byte]string{dhcpOffer: "offered", dhcpAck: "acked", dhcpNak: "refused"}

// lease is the single address the link hands out: the computer on the other
// end of the cable is the only client there can be.
type lease struct {
	server, client net.IP
	mask           net.IPMask
	time           time.Duration
}

// reply answers one client packet. It returns nil for anything that needs no
// answer: replies, releases, malformed packets, requests for another server.
func reply(req []byte, l lease) []byte {
	if len(req) < headerLen+len(magicCookie) || req[0] != bootRequest || string(req[headerLen:headerLen+4]) != string(magicCookie) {
		return nil
	}
	opts := options(req[headerLen+4:])
	kind := byte(0)
	if v := opts[optMsgType]; len(v) == 1 {
		kind = v[0]
	}
	if id, ok := opts[optServerID]; ok && !net.IP(id).Equal(l.server) {
		return nil
	}
	var answer byte
	yiaddr := l.client
	switch kind {
	case dhcpDiscover:
		answer = dhcpOffer
	case dhcpRequest:
		want := net.IP(opts[optRequested])
		if len(want) != 4 {
			want = net.IP(req[12:16])
		}
		if want.Equal(l.client) {
			answer = dhcpAck
		} else {
			answer, yiaddr = dhcpNak, net.IPv4zero.To4()
		}
	case dhcpInform:
		answer, yiaddr = dhcpAck, net.IPv4zero.To4()
	default:
		return nil
	}

	out := make([]byte, headerLen, minPacketLen)
	out[0], out[1], out[2] = bootReply, req[1], req[2]
	copy(out[4:8], req[4:8])
	copy(out[10:12], req[10:12])
	copy(out[12:16], req[12:16])
	copy(out[16:20], yiaddr.To4())
	if answer != dhcpNak {
		copy(out[20:24], l.server.To4())
	}
	copy(out[28:44], req[28:44])
	out = append(out, magicCookie...)
	out = append(out, optMsgType, 1, answer)
	out = append(out, optServerID, 4)
	out = append(out, l.server.To4()...)
	if answer != dhcpNak {
		out = append(out, optMask, 4)
		out = append(out, l.mask...)
		if kind != dhcpInform {
			secs := make([]byte, 4)
			binary.BigEndian.PutUint32(secs, uint32(l.time/time.Second))
			out = append(out, optLeaseTime, 4)
			out = append(out, secs...)
		}
	}
	out = append(out, optEnd)
	for len(out) < minPacketLen {
		out = append(out, 0)
	}
	return out
}

func options(b []byte) map[byte][]byte {
	opts := map[byte][]byte{}
	for i := 0; i < len(b); {
		code := b[i]
		if code == optEnd {
			break
		}
		if code == 0 {
			i++
			continue
		}
		if i+1 >= len(b) || i+2+int(b[i+1]) > len(b) {
			break
		}
		opts[code] = b[i+2 : i+2+int(b[i+1])]
		i += 2 + int(b[i+1])
	}
	return opts
}

type dhcpServer struct {
	conn net.PacketConn
}

// listenDHCP binds the DHCP server port on ifname alone, so the link never
// answers a client on any other network the device is on.
func listenDHCP(ifname string) (*dhcpServer, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			if serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); serr != nil {
				return
			}
			if serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); serr != nil {
				return
			}
			serr = syscall.BindToDevice(int(fd), ifname)
		})
		if err != nil {
			return err
		}
		return serr
	}}
	conn, err := lc.ListenPacket(context.Background(), "udp4", ":67")
	if err != nil {
		return nil, err
	}
	return &dhcpServer{conn: conn}, nil
}

func (s *dhcpServer) Serve(l lease) error {
	buf := make([]byte, 1500)
	to := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	for {
		n, _, err := s.conn.ReadFrom(buf)
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			return err
		}
		out := reply(buf[:n], l)
		if out == nil {
			continue
		}
		if _, err := s.conn.WriteTo(out, to); err != nil {
			return err
		}
		fmt.Printf("sinty-usb-link: %s %s to %s\n", kindName[out[headerLen+6]], net.IP(out[16:20]), net.HardwareAddr(out[28:34]))
	}
}

func (s *dhcpServer) Close() error {
	return s.conn.Close()
}
