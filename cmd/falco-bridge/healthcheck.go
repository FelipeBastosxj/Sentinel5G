package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// runHealthcheck implements the `falco-bridge healthcheck` subcommand,
// mirroring cmd/operator/healthcheck.go's runHealthcheck exactly: a plain
// HTTP GET against this same process's own /healthz, exiting 0 on 200 and 1
// otherwise. The final image is distroless (no shell/curl/wget), so
// HEALTHCHECK has nothing else it could exec against a running container.
func runHealthcheck() int {
	cfg := loadConfig()

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + cfg.listenAddr + "/healthz")
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
