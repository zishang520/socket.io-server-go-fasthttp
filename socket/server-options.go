package socket

import (
	"time"

	"github.com/zishang520/engine.io-server-go-fasthttp/v2/config"
	"github.com/zishang520/socket.io-go-parser/v2/parser"
)

type (
	ConnectionStateRecovery struct {
		// The backup duration of the sessions and the packets.
		maxDisconnectionDuration *int64

		// Whether to skip middlewares upon successful connection state recovery.
		skipMiddlewares *bool
	}

	ServerOptionsInterface interface {
		config.ServerOptionsInterface
		config.AttachOptionsInterface

		SetServeClient(bool)
		GetRawServeClient() *bool
		ServeClient() bool

		SetAdapter(AdapterConstructor)
		GetRawAdapter() AdapterConstructor
		Adapter() AdapterConstructor

		SetParser(parser.Parser)
		GetRawParser() parser.Parser
		Parser() parser.Parser

		SetConnectTimeout(time.Duration)
		GetRawConnectTimeout() *time.Duration
		ConnectTimeout() time.Duration

		SetConnectionStateRecovery(*ConnectionStateRecovery)
		GetRawConnectionStateRecovery() *ConnectionStateRecovery
		ConnectionStateRecovery() *ConnectionStateRecovery

		SetCleanupEmptyChildNamespaces(bool)
		GetRawCleanupEmptyChildNamespaces() *bool
		CleanupEmptyChildNamespaces() bool
	}

	ServerOptions struct {
		config.ServerOptions
		config.AttachOptions

		// whether to serve the client files
		serveClient *bool

		// the adapter to use
		adapter AdapterConstructor

		// the parser to use
		parser parser.Parser

		// how many ms before a client without namespace is closed
		connectTimeout *time.Duration

		// Whether to enable the recovery of connection state when a client temporarily disconnects.
		//
		// The connection state includes the missed packets, the rooms the socket was in and the `data` attribute.
		connectionStateRecovery *ConnectionStateRecovery

		// Whether to remove child namespaces that have no sockets connected to them
		cleanupEmptyChildNamespaces *bool
	}
)

func (c *ConnectionStateRecovery) SetMaxDisconnectionDuration(maxDisconnectionDuration int64) {
	c.maxDisconnectionDuration = &maxDisconnectionDuration
}
func (c *ConnectionStateRecovery) GetRawMaxDisconnectionDuration() *int64 {
	return c.maxDisconnectionDuration
}
func (c *ConnectionStateRecovery) MaxDisconnectionDuration() int64 {
	if c.maxDisconnectionDuration == nil {
		return 120000
	}

	return *c.maxDisconnectionDuration
}

func (c *ConnectionStateRecovery) SetSkipMiddlewares(skipMiddlewares bool) {
	c.skipMiddlewares = &skipMiddlewares
}
func (c *ConnectionStateRecovery) GetRawSkipMiddlewares() *bool {
	return c.skipMiddlewares
}
func (c *ConnectionStateRecovery) SkipMiddlewares() bool {
	if c.skipMiddlewares == nil {
		return true
	}

	return *c.skipMiddlewares
}

func DefaultServerOptions() *ServerOptions {
	a := &ServerOptions{}
	return a
}

func (s *ServerOptions) Assign(data ServerOptionsInterface) (ServerOptionsInterface, error) {
	if data == nil {
		return s, nil
	}

	if s.GetRawPingTimeout() == nil {
		s.SetPingTimeout(data.PingTimeout())
	}
	if s.GetRawPingInterval() == nil {
		s.SetPingInterval(data.PingInterval())
	}
	if s.GetRawUpgradeTimeout() == nil {
		s.SetUpgradeTimeout(data.UpgradeTimeout())
	}
	if s.GetRawMaxHttpBufferSize() == nil {
		s.SetMaxHttpBufferSize(data.MaxHttpBufferSize())
	}
	if s.GetRawAllowRequest() == nil {
		s.SetAllowRequest(data.AllowRequest())
	}
	if s.GetRawTransports() == nil {
		s.SetTransports(data.Transports())
	}
	if s.GetRawAllowUpgrades() == nil {
		s.SetAllowUpgrades(data.AllowUpgrades())
	}
	if s.GetRawPerMessageDeflate() == nil {
		s.SetPerMessageDeflate(data.PerMessageDeflate())
	}
	if s.GetRawHttpCompression() == nil {
		s.SetHttpCompression(data.HttpCompression())
	}
	if s.GetRawInitialPacket() == nil {
		s.SetInitialPacket(data.InitialPacket())
	}
	if s.GetRawCookie() == nil {
		s.SetCookie(data.Cookie())
	}
	if s.GetRawCors() == nil {
		s.SetCors(data.Cors())
	}
	if s.GetRawAllowEIO3() == nil {
		s.SetAllowEIO3(data.AllowEIO3())
	}

	if s.GetRawPath() == nil {
		s.SetPath(data.Path())
	}

	if s.GetRawDestroyUpgradeTimeout() == nil {
		s.SetDestroyUpgradeTimeout(data.DestroyUpgradeTimeout())
	}

	if s.GetRawDestroyUpgrade() == nil {
		s.SetDestroyUpgrade(data.DestroyUpgrade())
	}

	if s.GetRawAddTrailingSlash() == nil {
		s.SetAddTrailingSlash(data.AddTrailingSlash())
	}

	if s.GetRawServeClient() == nil {
		s.SetServeClient(data.ServeClient())
	}

	if s.GetRawAdapter() == nil {
		s.SetAdapter(data.Adapter())
	}

	if s.GetRawParser() == nil {
		s.SetParser(data.Parser())
	}

	if s.GetRawConnectTimeout() == nil {
		s.SetConnectTimeout(data.ConnectTimeout())
	}

	return s, nil
}

func (s *ServerOptions) Path() string {
	if s.GetRawPath() == nil {
		return "/socket.io"
	}

	return s.AttachOptions.Path()
}

func (s *ServerOptions) SetServeClient(serveClient bool) {
	s.serveClient = &serveClient
}
func (s *ServerOptions) GetRawServeClient() *bool {
	return s.serveClient
}
func (s *ServerOptions) ServeClient() bool {
	if s.serveClient == nil {
		return true
	}

	return *s.serveClient
}

func (s *ServerOptions) SetAdapter(adapter AdapterConstructor) {
	s.adapter = adapter
}
func (s *ServerOptions) GetRawAdapter() AdapterConstructor {
	return s.adapter
}
func (s *ServerOptions) Adapter() AdapterConstructor {
	if s.adapter == nil {
		return &AdapterBuilder{}
	}
	return s.adapter
}

func (s *ServerOptions) SetParser(parser parser.Parser) {
	s.parser = parser
}
func (s *ServerOptions) GetRawParser() parser.Parser {
	return s.parser
}
func (s *ServerOptions) Parser() parser.Parser {
	if s.parser == nil {
		return parser.NewParser()
	}
	return s.parser
}

func (s *ServerOptions) SetConnectTimeout(connectTimeout time.Duration) {
	s.connectTimeout = &connectTimeout
}
func (s *ServerOptions) GetRawConnectTimeout() *time.Duration {
	return s.connectTimeout
}
func (s *ServerOptions) ConnectTimeout() time.Duration {
	if s.connectTimeout == nil {
		return time.Duration(45_000 * time.Millisecond)
	}

	return *s.connectTimeout
}

func (s *ServerOptions) SetConnectionStateRecovery(connectionStateRecovery *ConnectionStateRecovery) {
	s.connectionStateRecovery = connectionStateRecovery
}
func (s *ServerOptions) GetRawConnectionStateRecovery() *ConnectionStateRecovery {
	return s.connectionStateRecovery
}
func (s *ServerOptions) ConnectionStateRecovery() *ConnectionStateRecovery {
	if s.connectionStateRecovery == nil {
		return &ConnectionStateRecovery{}
	}

	return s.connectionStateRecovery
}

func (s *ServerOptions) SetCleanupEmptyChildNamespaces(cleanupEmptyChildNamespaces bool) {
	s.cleanupEmptyChildNamespaces = &cleanupEmptyChildNamespaces
}
func (s *ServerOptions) GetRawCleanupEmptyChildNamespaces() *bool {
	return s.cleanupEmptyChildNamespaces
}
func (s *ServerOptions) CleanupEmptyChildNamespaces() bool {
	if s.cleanupEmptyChildNamespaces == nil {
		return false
	}

	return *s.cleanupEmptyChildNamespaces
}
