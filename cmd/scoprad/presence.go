package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/SynergyFlowLabs/scopra/internal/identity"
	"github.com/SynergyFlowLabs/scopra/internal/presence"
)

// startPresence watches owner-only local metadata so the first successful
// `scopra init` is picked up without requiring a Mio restart. Corrupt or
// missing cloud state disables only outbound presence; it never changes local
// workspace execution or starts an inbound listener.
func startPresence(ctx context.Context, stateDir string, stderr io.Writer) {
	go watchPresence(ctx, stateDir, stderr)
}

func watchPresence(ctx context.Context, stateDir string, stderr io.Writer) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastWarning time.Time
	warn := func(format string, values ...any) {
		if time.Since(lastWarning) < 5*time.Minute {
			return
		}
		lastWarning = time.Now()
		_, _ = fmt.Fprintf(stderr, "scoprad: warning: "+format+"\n", values...)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		config, err := identity.LoadCloudConfig(stateDir)
		if errors.Is(err, os.ErrNotExist) {
			// Local-only mode is the normal state before the user opts in.
		} else if err != nil {
			warn("cloud presence is disabled: %v", err)
		} else {
			key, keyErr := identity.LoadKey(stateDir)
			if keyErr != nil {
				warn("cloud presence is disabled because the machine key is unavailable: %v", keyErr)
			} else {
				presence.Run(ctx, config, key, func(format string, values ...any) {
					_, _ = fmt.Fprintf(stderr, "scoprad: "+format+"\n", values...)
				})
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
