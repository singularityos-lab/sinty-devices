// Package android reads what a phone's Android install leaves on its disk:
// the logical partitions inside the super partition.
package android

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	reservedBytes  = 4096
	geometrySize   = 4096
	geometryMagic  = 0x616c4467
	metadataMagic  = 0x414c5030
	partitionEntry = 52
	extentEntry    = 24
	targetLinear   = 0
	sectorSize     = 512
)

// Extent is a run of 512-byte sectors of the super partition.
type Extent struct {
	Start   uint64
	Sectors uint64
}

// Partition is a logical partition and where its data lies in super.
type Partition struct {
	Name    string
	Extents []Extent
}

// Size is the partition's size in bytes.
func (p Partition) Size() int64 {
	var n uint64
	for _, e := range p.Extents {
		n += e.Sectors
	}
	return int64(n * sectorSize)
}

// ReadSuper lists the logical partitions recorded in the metadata slot of a
// super partition, checking every checksum the format carries.
func ReadSuper(r io.ReaderAt, slot int) ([]Partition, error) {
	geo := make([]byte, geometrySize)
	if _, err := r.ReadAt(geo, reservedBytes); err != nil {
		return nil, fmt.Errorf("super geometry: %w", err)
	}
	le := binary.LittleEndian
	if le.Uint32(geo) != geometryMagic {
		return nil, errors.New("no logical partition geometry in super")
	}
	size := le.Uint32(geo[4:])
	if size < 52 || size > geometrySize || !checksumOK(geo[:size], 8) {
		return nil, errors.New("the super geometry fails its checksum")
	}
	maxSize, slots := le.Uint32(geo[40:]), le.Uint32(geo[44:])
	if slot < 0 || uint32(slot) >= slots {
		return nil, fmt.Errorf("super has %d metadata slots, no slot %d", slots, slot)
	}
	at := int64(reservedBytes + 2*geometrySize + int64(slot)*int64(maxSize))
	head := make([]byte, 256)
	if _, err := r.ReadAt(head, at); err != nil {
		return nil, fmt.Errorf("super metadata: %w", err)
	}
	if le.Uint32(head) != metadataMagic {
		return nil, fmt.Errorf("no logical partition metadata in slot %d", slot)
	}
	if major := le.Uint16(head[4:]); major != 10 {
		return nil, fmt.Errorf("logical partition metadata version %d is unknown", major)
	}
	headerSize := le.Uint32(head[8:])
	if headerSize < 128 || headerSize > 256 || !checksumOK(head[:headerSize], 12) {
		return nil, errors.New("the super metadata header fails its checksum")
	}
	tablesSize := le.Uint32(head[44:])
	if tablesSize > maxSize {
		return nil, errors.New("the super metadata tables overflow their slot")
	}
	tables := make([]byte, tablesSize)
	if _, err := r.ReadAt(tables, at+int64(headerSize)); err != nil {
		return nil, fmt.Errorf("super metadata tables: %w", err)
	}
	if sum := sha256.Sum256(tables); !bytes.Equal(sum[:], head[48:80]) {
		return nil, errors.New("the super metadata tables fail their checksum")
	}
	parts, err := table(tables, head[80:92], partitionEntry)
	if err != nil {
		return nil, fmt.Errorf("partition table: %w", err)
	}
	exts, err := table(tables, head[92:104], extentEntry)
	if err != nil {
		return nil, fmt.Errorf("extent table: %w", err)
	}
	var out []Partition
	for _, p := range parts {
		name := string(bytes.TrimRight(p[:36], "\x00"))
		first, n := le.Uint32(p[40:]), le.Uint32(p[44:])
		if uint64(first)+uint64(n) > uint64(len(exts)) {
			return nil, fmt.Errorf("%s lists extents past the extent table", name)
		}
		part := Partition{Name: name}
		for _, e := range exts[first : first+n] {
			if le.Uint32(e[8:]) != targetLinear || le.Uint32(e[20:]) != 0 {
				return nil, fmt.Errorf("%s has an extent outside super", name)
			}
			part.Extents = append(part.Extents, Extent{Start: le.Uint64(e[12:]), Sectors: le.Uint64(e[0:])})
		}
		out = append(out, part)
	}
	return out, nil
}

// Find returns the named partition among parts.
func Find(parts []Partition, name string) (Partition, bool) {
	for _, p := range parts {
		if p.Name == name {
			return p, true
		}
	}
	return Partition{}, false
}

func table(tables, desc []byte, entry int) ([][]byte, error) {
	le := binary.LittleEndian
	off, n, size := le.Uint32(desc), le.Uint32(desc[4:]), le.Uint32(desc[8:])
	if int(size) != entry {
		return nil, fmt.Errorf("entries of %d bytes, want %d", size, entry)
	}
	if uint64(off)+uint64(n)*uint64(size) > uint64(len(tables)) {
		return nil, errors.New("the table runs past the metadata")
	}
	out := make([][]byte, n)
	for i := range out {
		at := int(off) + i*entry
		out[i] = tables[at : at+entry]
	}
	return out, nil
}

// checksumOK checks a SHA-256 stored at sumAt inside b, computed over b with
// that field zeroed.
func checksumOK(b []byte, sumAt int) bool {
	c := bytes.Clone(b)
	clear(c[sumAt : sumAt+32])
	sum := sha256.Sum256(c)
	return bytes.Equal(sum[:], b[sumAt:sumAt+32])
}
