// Iris - Decentralized Messaging Framework
// Copyright 2014 Peter Szilagyi. All rights reserved.
//
// Iris is dual licensed: you can redistribute it and/or modify it under the
// terms of the GNU General Public License as published by the Free Software
// Foundation, either version 3 of the License, or (at your option) any later
// version.
//
// The framework is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.  See the GNU General Public License for
// more details.
//
// Alternatively, the Iris framework may be used in accordance with the terms
// and conditions contained in a signed written agreement between you and the
// author(s).
//
// Author: peterke@gmail.com (Peter Szilagyi)

package iris

import (
	"crypto/x509"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/karalabe/iris/config"
)

// Connection handler for the broadcast tests.
type broadcaster struct {
	msgs chan []byte
}

func (b *broadcaster) HandleBroadcast(msg []byte) {
	select {
	case b.msgs <- msg:
		// Ok
	default:
		panic("Broadcast queue full")
	}
}

func (b *broadcaster) HandleRequest(req []byte, timeout time.Duration) []byte {
	panic("Request passed to broadcast handler")
}

func (b *broadcaster) HandleTunnel(tun *Tunnel) {
	panic("Inbound tunnel on broadcast handler")
}

// Individual broadcast tests.
func TestBroadcastSingleNodeSingleConn(t *testing.T) {
	testBroadcast(t, 1, 1, 1000)
}

func TestBroadcastSingleNodeMultiConn(t *testing.T) {
	testBroadcast(t, 1, 10, 100)
}

func TestBroadcastMultiNodeSingleConn(t *testing.T) {
	testBroadcast(t, 10, 1, 100)
}

func TestBroadcastMultiNodeMultiConn(t *testing.T) {
	testBroadcast(t, 10, 10, 10)
}

// Tests multi node multi connection broadcasting.
func testBroadcast(t *testing.T, nodes, conns, msgs int) {
	// Configure the test
	swapConfigs()
	defer swapConfigs()

	olds := config.BootPorts
	for i := 0; i < nodes; i++ {
		config.BootPorts = append(config.BootPorts, 65000+i)
	}
	defer func() { config.BootPorts = olds }()

	key, _ := x509.ParsePKCS1PrivateKey(privKeyDer)
	overlay := "broadcast-test"
	cluster := fmt.Sprintf("broadcast-test-%d-%d", nodes, conns)

	// Boot the iris overlays
	liveNodes := make([]*Overlay, nodes)
	for i := 0; i < nodes; i++ {
		liveNodes[i] = New(overlay, key)
		if _, err := liveNodes[i].Boot(); err != nil {
			t.Fatalf("failed to boot iris overlay: %v.", err)
		}
		defer func(node *Overlay) {
			if err := node.Shutdown(); err != nil {
				t.Fatalf("failed to terminate iris node: %v.", err)
			}
		}(liveNodes[i])
	}
	// Connect to all nodes with a lot of clients
	liveHands := make(map[int][]*broadcaster)
	liveConns := make(map[int][]*Connection)
	for i, node := range liveNodes {
		liveHands[i] = make([]*broadcaster, conns)
		liveConns[i] = make([]*Connection, conns)
		for j := 0; j < conns; j++ {
			liveHands[i][j] = &broadcaster{make(chan []byte, nodes*conns*msgs)}
			conn, err := node.Connect(cluster, liveHands[i][j])
			if err != nil {
				t.Fatalf("failed to connect to the iris overlay: %v.", err)
			}
			liveConns[i][j] = conn

			defer func(conn *Connection) {
				if err := conn.Close(); err != nil {
					t.Fatalf("failed to close iris connection: %v.", err)
				}
			}(liveConns[i][j])
		}
	}
	// Make sure there is a little time to propagate state and reports (TODO, fix this)
	if nodes > 1 {
		time.Sleep(3 * time.Second)
	}
	// Broadcast with each and every node in parallel
	pend := new(sync.WaitGroup)
	for i := 0; i < nodes; i++ {
		for j := 0; j < conns; j++ {
			pend.Add(1)
			go func(i, j int) {
				defer pend.Done()
				for k := 0; k < msgs; k++ {
					msg := []byte{byte(i), byte(j), byte(k)}
					if err := liveConns[i][j].Broadcast(cluster, msg); err != nil {
						t.Fatalf("failed to broadcast message: %v.", err)
					}
				}
			}(i, j)
		}
	}
	pend.Wait()

	// Wait a while for messages to propagate through network
	time.Sleep(250 * time.Millisecond)

	// Verify that all broadcasts succeeded
	for i := 0; i < nodes; i++ {
		for j := 0; j < conns; j++ {
			if n := len(liveHands[i][j].msgs); n != nodes*conns*msgs {
				t.Fatalf("broadcast/deliver count mismatch: have %d, want %d", n, nodes*conns*msgs)
			}
		}
	}
}
