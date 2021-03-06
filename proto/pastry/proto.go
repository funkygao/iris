// Iris - Decentralized Messaging Framework
// Copyright 2013 Peter Szilagyi. All rights reserved.
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

// Contains the wire protocol for the pastry overlay communication.

package pastry

import (
	"encoding/gob"
	"math/big"

	"github.com/karalabe/iris/proto"
)

// Pastry operation code type.
type opcode uint8

// Pastry operation types.
const (
	opNop     opcode = iota // Application layer message
	opJoin                  // Join request
	opRepair                // Routing table repair request
	opActive                // Heartbeat for an active peer
	opPassive               // Heartbeat for a passive peer
	opExchage               // Pastry state exchange
	opClose                 // Leave request
)

// Routing state exchange message.
type state struct {
	Addrs   map[string][]string // Known peers and their network addresses
	Version uint64              // Version counter to skip old messages
}

// Extra headers for the overlay.
type header struct {
	Meta  interface{} // Additional upper layer headers
	Op    opcode      // The operation to execute
	Dest  *big.Int    // Destination id
	State *state      // Routing table state exchange
}

// Make sure the header struct is registered with gob.
func init() {
	gob.Register(&header{})
}

// Simple wrapper around the peer send method, to handle errors by dropping.
func (o *Overlay) send(msg *proto.Message, p *peer) {
	if err := p.send(msg); err != nil {
		o.drop(p)
	}
}

// Envelopes a pastry header into the generic packet container and sends it to
// its destination via the peer connection.
func (o *Overlay) sendPacket(dest *peer, head *header) {
	// Assemble and send the final message
	msg := &proto.Message{
		Head: proto.Header{
			Meta: head,
		},
	}
	if err := dest.send(msg); err != nil {
		o.drop(dest)
	}
}

// Assembles an overlay join message, consisting of the join opcode and local
// network addresses, sending it towards the destination node.
func (o *Overlay) sendJoin(dest *peer) {
	state := &state{
		Addrs: map[string][]string{o.nodeId.String(): o.addrs},
	}
	o.sendPacket(dest, &header{Op: opJoin, Dest: o.nodeId, State: state})
}

// Assembles an overlay repair request, consisting of the repair opcode and
// sends it towards the destination node.
func (o *Overlay) sendRepair(dest *peer) {
	o.sendPacket(dest, &header{Op: opRepair, Dest: o.nodeId})
}

// Assembles an overlay heartbeat message, consisting of the beat opcode and
// tagged whether the connection is an active route entry or not, sending it
// towards the destination node.
func (o *Overlay) sendBeat(dest *peer, passive bool) {
	if passive {
		o.sendPacket(dest, &header{Op: opPassive, Dest: dest.nodeId})
	} else {
		o.sendPacket(dest, &header{Op: opActive, Dest: dest.nodeId})
	}
}

// Assembles an overlay state message, consisting of the exchange opcode, the
// current version of the routing table and the peer addresses deemed needed,
// sending it towards the destination.
func (o *Overlay) sendState(dest *peer) {
	o.lock.RLock()

	s := &state{
		Addrs:   make(map[string][]string),
		Version: o.time,
	}

	// Serialize our own addresses, the leaf set and common row
	s.Addrs[o.nodeId.String()] = o.addrs
	for _, id := range o.routes.leaves {
		sid := id.String()
		if node, ok := o.livePeers[sid]; ok {
			s.Addrs[sid] = node.addrs
		}
	}
	idx, _ := prefix(o.nodeId, dest.nodeId)
	for _, id := range o.routes.routes[idx] {
		if id != nil {
			sid := id.String()
			if node, ok := o.livePeers[sid]; ok {
				s.Addrs[sid] = node.addrs
			}
		}
	}
	o.lock.RUnlock()

	// Send the state exchange
	o.sendPacket(dest, &header{Op: opExchage, Dest: dest.nodeId, State: s})
}

// Assembles an overlay leave message, consisting of the close opcode and sends
// it towards the destination.
func (o *Overlay) sendClose(dest *peer) {
	o.sendPacket(dest, &header{Op: opClose, Dest: dest.nodeId})
}
