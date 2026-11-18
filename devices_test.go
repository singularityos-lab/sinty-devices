package devices

import (
	"bytes"
	"testing"

	"github.com/singularityos-lab/sinty-devices/profile"
)

func TestEmbeddedCatalog(t *testing.T) {
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) == 0 {
		t.Fatal("no profiles embedded")
	}
	ix, err := profile.BuildIndex(FS)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ix.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	have, err := FS.ReadFile("index.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(have, want) {
		t.Fatal("embedded index.json is out of date")
	}
}
