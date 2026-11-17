// Command check-openapi keeps the committed public API contract current and backward compatible.
//
// Default: fail when openapi/v1.json differs from the generated contract, when the generated
// contract breaks the frozen baseline openapi/v1-baseline.json, or when info.version leaves 1.x.
// -write regenerates openapi/v1.json. -accept-baseline advances the baseline to the generated
// contract and refuses when that would drop anything a /v1 client may rely on.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/Contictus/launchtap/backend/internal/apicompat"
	"github.com/Contictus/launchtap/backend/internal/apiserver"
)

const (
	contractPath = "openapi/v1.json"
	baselinePath = "openapi/v1-baseline.json"
	pathMajor    = "1"
)

func main() {
	write := flag.Bool("write", false, "write the generated contract")
	acceptBaseline := flag.Bool("accept-baseline", false, "advance the compatibility baseline to the generated contract")
	flag.Parse()
	if err := run(*write, *acceptBaseline); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(write, acceptBaseline bool) error {
	generated, err := apiserver.GenerateOpenAPI()
	if err != nil {
		return err
	}
	major, err := apicompat.MajorVersion(generated)
	if err != nil {
		return err
	}
	if major != pathMajor {
		return fmt.Errorf("info.version major %s does not match the /v%s path; serve a breaking change under a new path version", major, pathMajor)
	}
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		return err
	}
	changes, err := apicompat.Compare(baseline, generated)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		fmt.Fprintf(os.Stderr, "%d breaking change(s) against %s:\n", len(changes), baselinePath)
		for _, change := range changes {
			fmt.Fprintln(os.Stderr, "  "+change.String())
		}
		return fmt.Errorf("the generated contract is not backward compatible with /v%s (docs/api/README.md)", pathMajor)
	}
	if acceptBaseline {
		return os.WriteFile(baselinePath, generated, 0o644)
	}
	if write {
		return os.WriteFile(contractPath, generated, 0o644)
	}
	committed, err := os.ReadFile(contractPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(generated, committed) {
		return fmt.Errorf("%s is stale; run check-openapi -write", contractPath)
	}
	return nil
}
