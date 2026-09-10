package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/config"
)

// runHealthcheck implements the `manager healthcheck` subcommand: a plain
// HTTP GET against this same process's own /healthz endpoint, exiting 0 on
// a 200 response and 1 otherwise. This is what the Dockerfile's HEALTHCHECK
// instruction actually runs -- the final image is gcr.io/distroless/static,
// which has no shell and no curl/wget, so HEALTHCHECK has nothing else it
// could exec against a *running* container besides this binary itself.
//
// Reads config.Load() (the same config the running server process loaded)
// rather than hardcoding ":8081", so a HEALTH_PROBE_BIND_ADDRESS override
// doesn't silently break the healthcheck.
func runHealthcheck() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: invalid configuration: %v\n", err)
		return 1
	}

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + cfg.HealthProbeBindAddress + "/healthz")
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: unexpected status %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
