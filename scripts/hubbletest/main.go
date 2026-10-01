//go:build ignore

// Throwaway harness: runs pkg/hubble.Observer against a REAL Hubble Relay
// and prints the NormalizedEvents it derives from real Cilium flows. Used
// once to close ROADMAP.md Phase 4's "pkg/hubble never run against a real
// daemon" item; not part of the build (build tag `ignore`).
//
//	go run scripts/hubbletest/main.go -addr localhost:4245
package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"encoding/json"
	"sync/atomic"

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"

	"github.com/FelipeBastosxj/Sentinel5G/pkg/events"
	"github.com/FelipeBastosxj/Sentinel5G/pkg/hubble"
)

func main() {
	addr := flag.String("addr", "localhost:4245", "Hubble Relay gRPC address")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	seconds := flag.Int("seconds", 20, "how long to observe")
	flag.Parse()

	cfg := events.DefaultConfig()
	cfg.URL = *natsURL
	cfg.StreamName = "HUBBLETEST"
	cfg.EventsSubject = "hubbletest.events"
	cfg.ThreatsSubject = "hubbletest.threats"
	cfg.ConnectTimeout = 3 * time.Second
	bus, err := events.Connect(cfg)
	if err != nil {
		panic(fmt.Sprintf("connect NATS: %v", err))
	}
	defer bus.Close()

	reader, err := nats.Connect(*natsURL)
	if err != nil {
		panic(fmt.Sprintf("reader connect: %v", err))
	}
	defer reader.Close()
	var got int64
	sub, err := reader.Subscribe(cfg.EventsSubject, func(m *nats.Msg) {
		var e events.NormalizedEvent
		if json.Unmarshal(m.Data, &e) != nil {
			return
		}
		n := atomic.AddInt64(&got, 1)
		if n <= 8 {
			fmt.Printf("HUBBLE->NORMALIZED: src=%s dst=%s dport=%d proto=%s pod=%s/%s node=%s\n",
				e.SourceIP, e.DestIP, e.DestPort, e.Protocol, e.Namespace, e.PodName, e.NodeName)
		}
	})
	if err != nil {
		panic(fmt.Sprintf("subscribe: %v", err))
	}
	defer func() { _ = sub.Unsubscribe() }()
	_ = reader.Flush()

	obs := &hubble.Observer{
		Addr:    *addr,
		Bus:     events.NewConnectedConnector(bus),
		Subject: cfg.EventsSubject,
		Log:     logr.Discard(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*seconds)*time.Second)
	defer cancel()
	fmt.Printf("observing real Hubble flows at %s for %ds...\n", *addr, *seconds)
	_ = obs.Start(ctx)
	fmt.Printf("TOTAL NormalizedEvents derived from real Cilium flows: %d\n", atomic.LoadInt64(&got))
}
