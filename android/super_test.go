package android

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

type testPart struct {
	name    string
	extents []Extent
}

// buildSuper writes logical partition metadata the way liblp lays it out:
// geometry twice after the reserved bytes, then one metadata copy per slot.
func buildSuper(t *testing.T, parts []testPart, slots int) []byte {
	t.Helper()
	le := binary.LittleEndian
	const maxSize = 65536
	var ptab, etab bytes.Buffer
	n := 0
	for _, p := range parts {
		e := make([]byte, partitionEntry)
		copy(e, p.name)
		le.PutUint32(e[40:], uint32(n))
		le.PutUint32(e[44:], uint32(len(p.extents)))
		ptab.Write(e)
		for _, x := range p.extents {
			x2 := make([]byte, extentEntry)
			le.PutUint64(x2[0:], x.Sectors)
			le.PutUint64(x2[12:], x.Start)
			etab.Write(x2)
			n++
		}
	}
	tables := append(ptab.Bytes(), etab.Bytes()...)
	head := make([]byte, 256)
	le.PutUint32(head, metadataMagic)
	le.PutUint16(head[4:], 10)
	le.PutUint16(head[6:], 2)
	le.PutUint32(head[8:], 256)
	le.PutUint32(head[44:], uint32(len(tables)))
	sum := sha256.Sum256(tables)
	copy(head[48:], sum[:])
	le.PutUint32(head[80:], 0)
	le.PutUint32(head[84:], uint32(len(parts)))
	le.PutUint32(head[88:], partitionEntry)
	le.PutUint32(head[92:], uint32(ptab.Len()))
	le.PutUint32(head[96:], uint32(n))
	le.PutUint32(head[100:], extentEntry)
	hs := sha256.Sum256(head)
	copy(head[12:], hs[:])

	geo := make([]byte, geometrySize)
	le.PutUint32(geo, geometryMagic)
	le.PutUint32(geo[4:], 52)
	le.PutUint32(geo[40:], maxSize)
	le.PutUint32(geo[44:], uint32(slots))
	le.PutUint32(geo[48:], 4096)
	gs := sha256.Sum256(geo[:52])
	copy(geo[8:], gs[:])

	img := make([]byte, reservedBytes+2*geometrySize+slots*maxSize)
	copy(img[reservedBytes:], geo)
	copy(img[reservedBytes+geometrySize:], geo)
	for s := 0; s < slots; s++ {
		at := reservedBytes + 2*geometrySize + s*maxSize
		copy(img[at:], head)
		copy(img[at+256:], tables)
	}
	return img
}

func TestReadSuperListsTheLogicalPartitions(t *testing.T) {
	img := buildSuper(t, []testPart{
		{"system_a", []Extent{{2048, 100}}},
		{"vendor_a", []Extent{{4096, 8}, {9000, 16}}},
		{"vendor_b", nil},
	}, 3)
	parts, err := ReadSuper(bytes.NewReader(img), 1)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := Find(parts, "vendor_a")
	if !ok || len(v.Extents) != 2 || v.Extents[1] != (Extent{9000, 16}) || v.Size() != 24*512 {
		t.Fatalf("vendor_a %+v", v)
	}
	if b, _ := Find(parts, "vendor_b"); b.Size() != 0 {
		t.Fatalf("vendor_b %+v", b)
	}
}

func TestReadSuperRefusesDamagedMetadata(t *testing.T) {
	img := buildSuper(t, []testPart{{"vendor_a", []Extent{{4096, 8}}}}, 2)
	tables := reservedBytes + 2*geometrySize + 256
	img[tables] ^= 1
	if _, err := ReadSuper(bytes.NewReader(img), 0); err == nil {
		t.Fatal("damaged tables were accepted")
	}
	img = buildSuper(t, []testPart{{"vendor_a", []Extent{{4096, 8}}}}, 2)
	if _, err := ReadSuper(bytes.NewReader(img), 2); err == nil {
		t.Fatal("a slot past the geometry was accepted")
	}
	img[reservedBytes+44] = 9
	if _, err := ReadSuper(bytes.NewReader(img), 0); err == nil {
		t.Fatal("a geometry with a bad checksum was accepted")
	}
}
