// Command minimal is the smallest possible gox service.
//
// Phase 0 placeholder: it only imports the root package so that
// scripts/check-deps.sh can prove a root-only import compiles none of the
// feature dependencies. Phase 1 replaces the body with gox.MustNew and one
// user component.
package main

import (
	"fmt"

	_ "github.com/guilhermebr/gox"
)

func main() {
	fmt.Println("gox minimal example")
}
