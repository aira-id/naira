// Package network implements domain.ConnectivityChecker.
package network

import (
	"context"
	"net"
	"time"
)

// Checker probes a small set of well-known DNS targets with a short timeout
// to decide "online or not" without depending on any specific external
// service being up.
type Checker struct {
	Targets []string
	Timeout time.Duration
}

// NewChecker returns a Checker with sane defaults.
func NewChecker() *Checker {
	return &Checker{
		Targets: []string{"1.1.1.1:443", "8.8.8.8:443"},
		Timeout: 2 * time.Second,
	}
}

func (c *Checker) Online(ctx context.Context) bool {
	d := net.Dialer{Timeout: c.Timeout}
	for _, target := range c.Targets {
		conn, err := d.DialContext(ctx, "tcp", target)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}
