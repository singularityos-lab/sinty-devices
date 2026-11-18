package devices

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/singularityos-lab/sinty-devices/profile"
)

//go:embed index.json devices
var FS embed.FS

func Profiles() ([]*profile.Profile, error) {
	ix, err := profile.BuildIndex(FS)
	if err != nil {
		return nil, err
	}
	out := make([]*profile.Profile, 0, len(ix.Devices))
	for _, e := range ix.Devices {
		data, err := fs.ReadFile(FS, e.Path)
		if err != nil {
			return nil, err
		}
		p, err := profile.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Path, err)
		}
		out = append(out, p)
	}
	return out, nil
}
