package socket

import (
	"time"

	"github.com/zishang520/engine.io-go-parser/packet"
	"github.com/zishang520/engine.io/v2/events"
	"github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/socket.io-go-parser/v2/parser"
)

type (
	// A public ID, sent by the server at the beginning of the Socket.IO session and which can be used for private messaging
	SocketId string

	// A private ID, sent by the server at the beginning of the Socket.IO session and used for connection state recovery
	// upon reconnection
	PrivateSessionId string

	// we could extend the Room type to "string", but that would be a breaking change
	// Related: https://github.com/socketio/socket.io-redis-adapter/issues/418
	Room string

	WriteOptions struct {
		packet.Options

		Volatile   bool `json:"volatile" mapstructure:"volatile" msgpack:"volatile"`
		PreEncoded bool `json:"preEncoded" mapstructure:"preEncoded" msgpack:"preEncoded"`
	}

	BroadcastFlags struct {
		WriteOptions

		Local     bool           `json:"local" mapstructure:"local" msgpack:"local"`
		Broadcast bool           `json:"broadcast" mapstructure:"broadcast" msgpack:"broadcast"`
		Binary    bool           `json:"binary" mapstructure:"binary" msgpack:"binary"`
		Timeout   *time.Duration `json:"timeout,omitempty" mapstructure:"timeout,omitempty" msgpack:"timeout,omitempty"`

		ExpectSingleResponse bool `json:"expectSingleResponse" mapstructure:"expectSingleResponse" msgpack:"expectSingleResponse"`
	}

	BroadcastOptions struct {
		Rooms  *types.Set[Room]
		Except *types.Set[Room]
		Flags  *BroadcastFlags `json:"flags,omitempty" mapstructure:"flags,omitempty" msgpack:"flags,omitempty"`
	}

	SessionToPersist struct {
		Sid   SocketId         `json:"sid" mapstructure:"sid" msgpack:"sid"`
		Pid   PrivateSessionId `json:"pid" mapstructure:"pid" msgpack:"pid"`
		Rooms *types.Set[Room]
		Data  any `json:"data" mapstructure:"data" msgpack:"data"`
	}

	Session struct {
		*SessionToPersist

		MissedPackets []any `json:"missedPackets" mapstructure:"missedPackets" msgpack:"missedPackets"`
	}

	PersistedPacket struct {
		Id        string            `json:"id" mapstructure:"id" msgpack:"id"`
		EmittedAt int64             `json:"emittedAt" mapstructure:"emittedAt" msgpack:"emittedAt"`
		Data      any               `json:"data" mapstructure:"data" msgpack:"data"`
		Opts      *BroadcastOptions `json:"opts,omitempty" mapstructure:"opts,omitempty" msgpack:"opts,omitempty"`
	}

	SessionWithTimestamp struct {
		*SessionToPersist

		DisconnectedAt int64 `json:"disconnectedAt" mapstructure:"disconnectedAt" msgpack:"disconnectedAt"`
	}

	Adapter interface {
		events.EventEmitter

		// #prototype

		Prototype(Adapter)
		Proto() Adapter

		Rooms() *types.Map[Room, *types.Set[SocketId]]
		Sids() *types.Map[SocketId, *types.Set[Room]]
		Nsp() NamespaceInterface

		// Construct() should be called after calling Prototype()
		Construct(NamespaceInterface)

		// To be overridden
		Init()

		// To be overridden
		Close()

		// Returns the number of Socket.IO servers in the cluster
		ServerCount() int64

		// Adds a socket to a list of room.
		AddAll(SocketId, *types.Set[Room])

		// Removes a socket from a room.
		Del(SocketId, Room)

		// Removes a socket from all rooms it's joined.
		DelAll(SocketId)

		// Broadcasts a packet.
		//
		// Options:
		//  - `Flags` {*BroadcastFlags} flags for this packet
		//  - `Except` {*types.Set[Room]} sids that should be excluded
		//  - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
		Broadcast(*parser.Packet, *BroadcastOptions)

		// Broadcasts a packet and expects multiple acknowledgements.
		//
		// Options:
		//  - `Flags` {*BroadcastFlags} flags for this packet
		//  - `Except` {*types.Set[Room]} sids that should be excluded
		//  - `Rooms` {*types.Set[Room]} list of rooms to broadcast to
		BroadcastWithAck(*parser.Packet, *BroadcastOptions, func(uint64), func([]any, error))

		// Gets a list of sockets by sid.
		Sockets(*types.Set[Room]) *types.Set[SocketId]

		// Gets the list of rooms a given socket has joined.
		SocketRooms(SocketId) *types.Set[Room]

		// Returns the matching socket instances
		FetchSockets(*BroadcastOptions) func(func([]SocketDetails, error))

		// Makes the matching socket instances join the specified rooms
		AddSockets(*BroadcastOptions, []Room)

		// Makes the matching socket instances leave the specified rooms
		DelSockets(*BroadcastOptions, []Room)

		// Makes the matching socket instances disconnect
		DisconnectSockets(*BroadcastOptions, bool)

		// Send a packet to the other Socket.IO servers in the cluster
		ServerSideEmit([]any) error

		// Save the client session in order to restore it upon reconnection.
		PersistSession(*SessionToPersist)

		// Restore the session and find the packets that were missed by the client.
		RestoreSession(PrivateSessionId, string) (*Session, error)
	}

	AdapterConstructor interface {
		New(NamespaceInterface) Adapter
	}
)
