// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package mesh implements a two node, master-slave network for the dex.
// Clients can connect to either node. Only the master originates events.
// Both nodes apply them to their local application state. Client requests
// that change the state are executed on the master, so if the client is
// connected to the slave, the request is forwarded to the master. State
// updates (events) are streamed from the master to the slave.
//
// Two important concepts in the system are Commands and Events. Commands are
// client-originating requests that may change the state of the system. A
// Command may be resolved by emitting an Event, by responding to the client
// with a result without emitting an Event (for example if this was a
// duplicate Command that already has a result), or by responding with a
// failure. In all three cases it is mesh that sends the response to the
// client, through whichever node the client is connected to. A response is
// delivered at most once. A forwarded command whose outcome is unknown may
// receive ResultUnavailableError. A lost client connection can prevent any
// response from being delivered.
//
// Events are state updates that happen either as a result of a Command, or
// are created by the master directly (a MasterWorker calling ApplyEvent).
// Every Event is appended to a durable event log, and the slave applies the
// Events in log order. This way two nodes with the same event log will have
// the same state.
//
// Service is the entry point to the mesh package. With both ListenAddr
// and PeerAddr empty, it runs as a single server.
//
// If we are running as a two-node mesh, the node will remain in a pending
// state until it connects to its peer and completes a handshake. A pending
// node does not serve clients. A node with an empty log has ten minutes to
// finish seeding, including waiting for its peer. A node with existing history
// waits for its peer until startup is canceled or fails. The handshake compares
// configurations and event logs to decide if the nodes are compatible and
// which node should be master.
// A node with an empty event log loads a snapshot when joining a master
// with existing history, then follows its event stream.
//
// A pending node halts if its outbound handshake reports permanent
// configuration incompatibility. An established master rejects the
// incompatible peer and keeps serving.
// A node will also halt if its event log has diverged from the serving
// master's, in which case the master keeps serving. If both nodes are master
// and their event logs have diverged, both will halt. A halt is terminal:
// the node stops serving and has to be restarted.
//
// During graceful shutdown, the master waits for the slave to catch up
// and requests handoff. Shutdown continues if either step fails or times out.
// Without handoff, an established slave promotes only after the full
// promotion delay without reconnection or fresh evidence of a live master.
// A slave that loses its master during initial catch-up returns to pending
// and does not promote on a timer.
package mesh
