package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const DevicesDir = "devices"

func BuildIndex(fsys fs.FS) (*Index, error) {
	ix := &Index{Schema: Schema, Devices: []Entry{}}
	var errs []error
	seen := map[string]string{}
	err := fs.WalkDir(fsys, DevicesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if depth := strings.Count(p, "/"); depth > 1 {
				errs = append(errs, fmt.Errorf("%s: profiles live in %s/<vendor>/<model>.json", p, DevicesDir))
				return fs.SkipDir
			}
			return nil
		}
		if path.Ext(p) != ".json" || strings.Count(p, "/") != 2 {
			errs = append(errs, fmt.Errorf("%s: profiles live in %s/<vendor>/<model>.json", p, DevicesDir))
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		pr, err := Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			return nil
		}
		want := strings.TrimSuffix(strings.TrimPrefix(p, DevicesDir+"/"), ".json")
		if pr.ID != want {
			errs = append(errs, fmt.Errorf("%s: id is %q, the path says %q", p, pr.ID, want))
			return nil
		}
		if other, dup := seen[pr.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: id %q already used by %s", p, pr.ID, other))
			return nil
		}
		seen[pr.ID] = p
		ix.Devices = append(ix.Devices, Entry{
			ID:              pr.ID,
			Path:            p,
			Vendor:          pr.Vendor,
			Model:           pr.Model,
			MinFlashVersion: pr.MinFlashVersion,
			SHA256:          Digest(data),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	sort.Slice(ix.Devices, func(i, j int) bool { return ix.Devices[i].ID < ix.Devices[j].ID })
	return ix, nil
}

func (ix *Index) Marshal() ([]byte, error) {
	b, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
