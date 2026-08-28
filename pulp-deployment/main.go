// pulp-deployment is the Pulp host binary for Bunch.
// It imports the required capability extensions (HTTP + SQLite) and calls
// run.Main(), which loads the composed Bunch application at runtime.
// Build with: go build -o bunch-deployment . (native host, not WASM)
// Then run:   ./bunch-deployment --app ../application/pulp.app.toml
package main

import (
	_ "github.com/BananaLabs-OSS/Pulp-ext-entropy"
	_ "github.com/BananaLabs-OSS/Pulp-ext-http"
	_ "github.com/BananaLabs-OSS/Pulp-ext-sqlite"

	"github.com/BananaLabs-OSS/Pulp/run"
)

func main() { run.Main() }
