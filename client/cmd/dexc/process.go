// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
The process server is a tiny mute tcp server that is used to handle multiple
invocations of dexc. If window-mode is not disabled in dexc config, the
connection to the process server should be attempted. If the connection is
successful, that means a windowed instance of dexc is already running and the
new session should be aborted. If no process server is found, the one should be
started by the new windowed session.
*/

package main

import (
	"context"
	"net"
	"time"
)

const processAddr = ":57580"

// runProcessServer starts a new process server. The returned channel will be
// signaled any time the process server is hit by another dexc instance.
func runProcessServer() (<-chan struct{}, error) {
	server, err := net.Listen("tcp", processAddr)
	if err != nil {
		return nil, err
	}
	go func() {
		<-appCtx.Done()
		server.Close()
	}()
	c := make(chan struct{})
	go func() {
		for {
			conn, err := server.Accept()
			if err != nil {
				log.Tracef("Accept error: %v", err)
				return
			}
			conn.Close()
			select {
			case c <- struct{}{}:
			default:
			}

		}
	}()

	return c, nil
}

// processServerRunning returns true if an attempt to connect to the process
// server succeeds.
func processServerRunning() bool {
	var d net.Dialer
	ctx, cancel := context.WithTimeout(appCtx, time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", processAddr)
	if err != nil {
		log.Tracef("DialContext error:", err)
		return false
	}
	conn.Close()
	return true
}
