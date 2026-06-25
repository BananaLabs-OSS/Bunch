// pulp-deployment is the Pulp host binary for Bunch.
// It imports the required capability extensions (HTTP + SQLite) and calls
// run.Main(), which loads bunch.wasm (the cell) at runtime.
// Build with: go build -o bunch-deployment . (native host, not WASM)
// Then run:   ./bunch-deployment --cell ../pulp-cell/bunch.wasm
package main

import (
	_ "github.com/BananaLabs-OSS/Pulp-ext-http"
	_ "github.com/BananaLabs-OSS/Pulp-ext-sqlite"

	"github.com/BananaLabs-OSS/Pulp/run"
)

func main() { run.Main() }
