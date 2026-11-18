package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/singularityos-lab/sinty-devices/profile"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fl := flag.NewFlagSet("validate", flag.ContinueOnError)
	write := fl.Bool("write", false, "rewrite index.json from the profiles")
	root := fl.String("root", ".", "repository root")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	ix, err := profile.BuildIndex(os.DirFS(*root))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	want, err := ix.Marshal()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	indexPath := filepath.Join(*root, "index.json")
	if *write {
		if err := os.WriteFile(indexPath, want, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("index.json: %d devices\n", len(ix.Devices))
		return 0
	}
	have, err := os.ReadFile(indexPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !bytes.Equal(have, want) {
		fmt.Fprintln(os.Stderr, "index.json is out of date: run go run ./cmd/validate -write")
		return 1
	}
	fmt.Printf("%d device profiles valid, index.json up to date\n", len(ix.Devices))
	return 0
}
