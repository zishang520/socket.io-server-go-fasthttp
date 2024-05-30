package socket

import (
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/savsgio/gotils/strconv"
	"github.com/valyala/fasthttp"
	"github.com/zishang520/engine.io-server-go-fasthttp/v2/engine"
	f_types "github.com/zishang520/engine.io-server-go-fasthttp/v2/types"
	"github.com/zishang520/engine.io/v2/log"
	"github.com/zishang520/engine.io/v2/types"
	"github.com/zishang520/engine.io/v2/utils"
	"github.com/zishang520/socket.io-go-parser/v2/parser"
	"github.com/zishang520/socket.io/v2/socket"
)

const clientVersion = "4.7.2"

var (
	dotMapRegex = regexp.MustCompile(`\.map`)
	server_log  = log.NewLog("socket.io:server")
)

type (
	ParentNspNameMatchFn *func(string, any, func(error, bool))

	// Represents a Socket.IO server.
	//
	//	import (
	//		"github.com/zishang520/engine.io/v2/utils"
	//		"github.com/zishang520/socket.io/v2/socket"
	//	)
	//
	//	io := socket.NewServer(nil, nil)
	//
	//	io.On("connection", func(clients ...any) {
	//		socket := clients[0].(*socket.Socket)
	//
	//		utils.Log().Info(`socket %s connected`, socket.Id())
	//
	//		// send an event to the client
	//		socket.Emit("foo", "bar")
	//
	//		socket.On("foobar", func(...any) {
	//			// an event was received from the client
	//		})
	//
	//		// upon disconnection
	//		socket.On("disconnect", func(reason ...any) {
	//			utils.Log().Info(`socket %s disconnected due to %s`, socket.Id(), reason[0])
	//		})
	//	})
	//	io.Listen(3000, nil)
	Server struct {
		*socket.StrictEventEmitter

		// #readonly
		sockets NamespaceInterface
		// A reference to the underlying Engine.IO server.
		//
		//	clientsCount := io.Engine().ClientsCount()
		engine engine.BaseServer
		// @private
		_parser parser.Parser
		// @private
		encoder parser.Encoder
		// @private
		_nsps *types.Map[string, *Namespace]
		// @private
		parentNsps *types.Map[ParentNspNameMatchFn, *ParentNamespace]
		// @private
		//
		// A subset of the {parentNsps} map, only containing {ParentNamespace} which are based on a regular
		// expression.
		parentNamespacesFromRegExp *types.Map[*regexp.Regexp, *ParentNamespace]
		_adapter                   AdapterConstructor
		_serveClient               bool
		// @private
		// #readonly
		opts            *ServerOptions
		eio             engine.Server
		_path           string
		clientPathRegex *regexp.Regexp
		// @private
		_connectTimeout time.Duration
		httpServer      *f_types.HttpServer
		_corsMiddleware engine.Middleware
	}
)

func MakeServer() *Server {
	s := &Server{
		_nsps:                      &types.Map[string, *Namespace]{},
		parentNsps:                 &types.Map[ParentNspNameMatchFn, *ParentNamespace]{},
		parentNamespacesFromRegExp: &types.Map[*regexp.Regexp, *ParentNamespace]{},
	}
	return s
}

func NewServer(srv any, opts *ServerOptions) *Server {
	s := MakeServer()

	s.Construct(srv, opts)

	return s
}

func (s *Server) Sockets() NamespaceInterface {
	return s.sockets
}

func (s *Server) Engine() engine.BaseServer {
	return s.engine
}

func (s *Server) Encoder() parser.Encoder {
	return s.encoder
}

func (s *Server) Construct(srv any, opts *ServerOptions) {
	if opts == nil {
		opts = DefaultServerOptions()
	}

	s.SetPath(opts.Path())
	s.SetConnectTimeout(opts.ConnectTimeout())
	s.SetServeClient(opts.ServeClient())
	if opts.GetRawParser() != nil {
		s._parser = opts.Parser()
	} else {
		s._parser = parser.NewParser()
	}
	s.encoder = s._parser.NewEncoder()
	s.opts = opts
	if opts.GetRawAdapter() != nil {
		s.SetAdapter(opts.Adapter())
	} else {
		if opts.GetRawConnectionStateRecovery() != nil {
			s.SetAdapter(&SessionAwareAdapterBuilder{})
		} else {
			s.SetAdapter(&AdapterBuilder{})
		}
	}
	s.sockets = s.Of("/", nil)

	s.StrictEventEmitter = s.sockets.EventEmitter()

	if srv != nil {
		s.Attach(srv, nil)
	}

	if s.opts.GetRawCors() != nil {
		s._corsMiddleware = f_types.MiddlewareWrapper(s.opts.Cors())
	}
}

func (s *Server) Opts() *ServerOptions {
	return s.opts
}

// Sets/gets whether client code is being served.
//
// Param: v - whether to serve client code
func (s *Server) SetServeClient(v bool) *Server {
	s._serveClient = v
	return s
}

// Return: self when setting or value when getting
func (s *Server) ServeClient() bool {
	return s._serveClient
}

// Executes the middleware for an incoming namespace not already created on the server.
//
// Param: name - name of incoming namespace
//
// Param: auth - the auth parameters
//
// Param: fn - callback
func (s *Server) _checkNamespace(name string, auth any, fn func(nsp *Namespace)) {
	end := true
	s.parentNsps.Range(func(nextFn ParentNspNameMatchFn, pnsp *ParentNamespace) bool {
		status := false
		(*nextFn)(name, auth, func(err error, allow bool) {
			if err != nil || !allow {
				status = true
				return
			}
			if nsp, ok := s._nsps.Load(name); ok {
				// the namespace was created in the meantime
				server_log.Debug("dynamic namespace %s already exists", name)
				fn(nsp)
				end = false
				return
			}
			namespace := pnsp.CreateChild(name)
			server_log.Debug("dynamic namespace %s was created", name)
			fn(namespace)
			end = false
		})
		return status // whether to continue traversing.
	})
	if end {
		fn(nil)
	}
}

// Sets the client serving path.
//
// Param: v pathname
func (s *Server) SetPath(v string) *Server {
	s._path = strings.TrimRight(v, "/")
	s.clientPathRegex = regexp.MustCompile(`^` + regexp.QuoteMeta(s._path) + `/socket\.io(\.msgpack|\.esm)?(\.min)?\.js(\.map)?(?:\?|$)`)
	return s
}

// Return: self when setting or value when getting
func (s *Server) Path() string {
	return s._path
}

// Set the delay after which a client without namespace is closed
//
// Param: v
func (s *Server) SetConnectTimeout(v time.Duration) *Server {
	s._connectTimeout = v
	return s
}

func (s *Server) ConnectTimeout() time.Duration {
	return s._connectTimeout
}

// Sets the adapter for rooms.
//
// Param: v AdapterConstructor interface
func (s *Server) SetAdapter(v AdapterConstructor) *Server {
	s._adapter = v
	s._nsps.Range(func(_ string, nsp *Namespace) bool {
		nsp.InitAdapter()
		return true
	})
	return s
}

func (s *Server) Adapter() AdapterConstructor {
	return s._adapter
}

// Attaches socket.io to a server or port.
//
// Param: srv - server or port
//
// Param: opts - options passed to engine.io
func (s *Server) Listen(srv any, opts *ServerOptions) *Server {
	return s.Attach(srv, opts)
}

// Attaches socket.io to a server or port.
//
// Param: srv - server or port
//
// Param: opts - options passed to engine.io
func (s *Server) Attach(srv any, opts *ServerOptions) *Server {
	var server *f_types.HttpServer
	switch address := srv.(type) {
	case int:
		_address := fmt.Sprintf(":%d", address)
		// handle a port as a int
		server_log.Debug("creating http server and binding to %s", _address)
		server = f_types.NewWebServer(f_types.HandlerFunc(func(ctx *fasthttp.RequestCtx) {
			ctx.Error("404 page not found", fasthttp.StatusNotFound)
		}))
		server.Listen(_address, nil)
	case string:
		// handle a port as a string
		server_log.Debug("creating http server and binding to %s", address)
		server = f_types.NewWebServer(f_types.HandlerFunc(func(ctx *fasthttp.RequestCtx) {
			ctx.Error("404 page not found", fasthttp.StatusNotFound)
		}))
		server.Listen(address, nil)
	case *f_types.HttpServer:
		server = address
	default:
		panic(errors.New(fmt.Sprintf("You are trying to attach socket.io to an express request handler %T. Please pass a *types.HttpServer instance.", address)))
	}
	if opts == nil {
		opts = DefaultServerOptions()
	}

	// merge the options passed to the Socket.IO server
	opts.Assign(s.opts)
	// set engine.io path to `/socket.io`
	if opts.GetRawPath() == nil {
		opts.SetPath(s._path)
	}
	s.initEngine(server, opts)

	return s
}

// Output fasthttp.Handler interface
func (s *Server) ServeHandler(opts *ServerOptions) f_types.Handler {
	// If an instance already exists, reuse it.
	if s.eio != nil {
		return s.eio
	}

	if opts == nil {
		opts = DefaultServerOptions()
	}

	// merge the options passed to the Socket.IO server
	opts.Assign(s.opts)
	// set engine.io path to `/socket.io`
	if opts.GetRawPath() == nil {
		opts.SetPath(s._path)
	}

	// initialize engine
	server_log.Debug("creating fasthttp.Handler-based engine with opts %v", opts)
	s.eio = engine.NewServer(opts)
	// bind to engine events
	s.Bind(s.eio)

	return s.eio
}

// Initialize engine
//
// Param: srv - the server to attach to
//
// Param: opts - options passed to engine.io
func (s *Server) initEngine(srv *f_types.HttpServer, opts *ServerOptions) {
	// initialize engine
	server_log.Debug("creating engine.io instance with opts %v", opts)
	s.eio = engine.Attach(srv, any(opts))

	// attach static file serving
	if s._serveClient {
		s.attachServe(srv, s.eio, opts)
	}

	// Export http server
	s.httpServer = srv

	// bind to engine events
	s.Bind(s.eio)
}

// Attaches the static file serving.
func (s *Server) attachServe(srv *f_types.HttpServer, egs engine.Server, opts *ServerOptions) {
	server_log.Debug("attaching client serving req handler")
	srv.HandleFunc(s._path+"/", func(ctx *fasthttp.RequestCtx) {
		if s.clientPathRegex.Match(ctx.Path()) {
			if s._corsMiddleware != nil {
				s._corsMiddleware(f_types.NewHttpContext(ctx), func(error) {
					s.serve(ctx)
				})
			} else {
				s.serve(ctx)
			}
		} else {
			if opts.AddTrailingSlash() {
				egs.FastHTTP(ctx)
			} else {
				srv.DefaultHandler.FastHTTP(ctx)
			}
		}
	})
}

// Handles a request serving of client source and map
func (s *Server) serve(ctx *fasthttp.RequestCtx) {
	if method := strconv.B2S(ctx.Method()); method != fasthttp.MethodGet && method != fasthttp.MethodHead {
		ctx.Error("Method Not Allowed", fasthttp.StatusMethodNotAllowed)
		return
	}
	filename := filepath.Base(strconv.B2S(ctx.Path()))
	isMap := dotMapRegex.MatchString(filename)
	_type := "source"
	if isMap {
		_type = "map"
	}
	// Per the standard, ETags must be quoted:
	// https://tools.ietf.org/html/rfc7232#section-2.3
	expectedEtag := `"` + clientVersion + `"`
	weakEtag := "W/" + expectedEtag

	if etag := strconv.B2S(ctx.Request.Header.Peek("If-None-Match")); etag != "" {
		if expectedEtag == etag || weakEtag == etag {
			server_log.Debug("serve client %s 304", _type)
			ctx.SetStatusCode(fasthttp.StatusNotModified)
			ctx.Write(nil)
			return
		}
	}

	server_log.Debug("serve client %s", _type)
	ctx.Response.Header.Set("Cache-Control", "public, max-age=0")
	if isMap {
		ctx.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
	} else {
		ctx.Response.Header.Set("Content-Type", "application/javascript; charset=utf-8")
	}
	ctx.Response.Header.Set("ETag", expectedEtag)
	s.sendFile(filename, ctx)
}

func (Server) sendFile(filename string, ctx *fasthttp.RequestCtx) {
	_file, err := os.Executable()
	if err != nil {
		server_log.Debug("Failed to get run path: %v", err)
		ctx.Error("file not found", fasthttp.StatusNotFound)
		return
	}
	// Construct the full, intended destination path
	basePath := filepath.Dir(filepath.Dir(_file))
	targetPath := filepath.Clean(filepath.Join(basePath, "client-dist", filename))

	// Verify the target path is still within the intended directory boundary
	if !strings.HasPrefix(targetPath, basePath) {
		ctx.Error("file not found", fasthttp.StatusNotFound)
		return
	}
	file, err := os.Open(targetPath)
	if err != nil {
		server_log.Debug("File read failed: %v", err)
		ctx.Error("file not found", fasthttp.StatusNotFound)
		return
	}
	defer file.Close()

	encoding := utils.Contains(strconv.B2S(ctx.Request.Header.Peek("Accept-Encoding")), []string{"gzip", "deflate", "br"})

	switch encoding {
	case "br":
		br := brotli.NewWriterLevel(ctx, 1)
		defer br.Close()
		ctx.Response.Header.Set("Content-Encoding", "br")
		ctx.SetStatusCode(fasthttp.StatusOK)
		io.Copy(br, file)
	case "gzip":
		gz, err := gzip.NewWriterLevel(ctx, 1)
		if err != nil {
			server_log.Debug("Failed to compress data: %v", err)
			ctx.Error("Internal Server Error", fasthttp.StatusInternalServerError)
			return
		}
		defer gz.Close()
		ctx.Response.Header.Set("Content-Encoding", "gzip")
		ctx.SetStatusCode(fasthttp.StatusOK)
		io.Copy(gz, file)
	case "deflate":
		fl, err := flate.NewWriter(ctx, 1)
		if err != nil {
			server_log.Debug("Failed to compress data: %v", err)
			ctx.Error("Internal Server Error", fasthttp.StatusInternalServerError)
			return
		}
		defer fl.Close()
		ctx.Response.Header.Set("Content-Encoding", "deflate")
		ctx.SetStatusCode(fasthttp.StatusOK)
		io.Copy(fl, file)
	default:
		ctx.SetStatusCode(fasthttp.StatusOK)
		io.Copy(ctx, file)
	}
}

// Binds socket.io to an engine.io instance.
//
// Param: engine engine.io (or compatible) server
func (s *Server) Bind(egs engine.BaseServer) *Server {
	s.engine = egs
	s.engine.On("connection", s.onconnection)
	return s
}

// Called with each incoming transport connection.
func (s *Server) onconnection(conns ...any) {
	conn := conns[0].(engine.Socket)
	server_log.Debug("incoming connection with id %s", conn.Id())
	client := NewClient(s, conn)
	if conn.Protocol() == 3 {
		client.connect("/", nil)
	}
}

// Looks up a namespace.
//
//	// with a simple string
//	myNamespace := io.Of("/my-namespace")
//
//	// with a regex
//	const dynamicNsp = io.Of(regexp.MustCompile(`^\/dynamic-\d+$`), nil).On("connection", func(args ...any) {
//		socket := args[0].(*socket.Socket)
//		namespace := socket.Nsp() // newNamespace.Name() === "/dynamic-101"
//
//		// broadcast to all clients in the given sub-namespace
//		namespace.Emit("hello")
//	})
//
// Param: string | *regexp.Regexp | ParentNspNameMatchFn - nsp name
//
// Param: func(...any) - nsp `connection` ev handler
func (s *Server) Of(name any, fn func(...any)) NamespaceInterface {
	switch n := name.(type) {
	case ParentNspNameMatchFn:
		parentNsp := NewParentNamespace(s)
		server_log.Debug("initializing parent namespace %s", parentNsp.Name())

		s.parentNsps.Store(n, parentNsp)

		if fn != nil {
			parentNsp.On("connect", fn)
		}
		return parentNsp
	case *regexp.Regexp:
		parentNsp := NewParentNamespace(s)
		server_log.Debug("initializing parent namespace %s", parentNsp.Name())

		nfn := func(nsp string, _ any, next func(error, bool)) {
			next(nil, n.MatchString(nsp))
		}
		s.parentNsps.Store(ParentNspNameMatchFn(&nfn), parentNsp)
		s.parentNamespacesFromRegExp.Store(n, parentNsp)

		if fn != nil {
			parentNsp.On("connect", fn)
		}
		return parentNsp
	}

	n, ok := name.(string)
	if ok {
		if len(n) > 0 {
			if n[0] != '/' {
				n = "/" + n
			}
		} else {
			n = "/"
		}
	} else {
		n = "/"
	}

	var namespace *Namespace

	if nsp, ok := s._nsps.Load(n); ok {
		namespace = nsp
	} else {
		s.parentNamespacesFromRegExp.Range(func(regex *regexp.Regexp, parentNamespace *ParentNamespace) bool {
			if regex.MatchString(n) {
				server_log.Debug("attaching namespace %s to parent namespace %s", n, regex.String())
				namespace = parentNamespace.CreateChild(n)
				return false
			}
			return true
		})

		if namespace != nil {
			return namespace
		}

		server_log.Debug("initializing namespace %s", n)
		namespace = NewNamespace(s, n)
		s._nsps.Store(n, namespace)
		if n != "/" {
			s.sockets.EmitReserved("new_namespace", namespace)
		}
	}

	if fn != nil {
		namespace.On("connect", fn)
	}
	return namespace
}

// Closes server connection
//
// Param: [fn] optional, called as `fn(error)` on error OR all conns closed
func (s *Server) Close(fn func(error)) {
	s._nsps.Range(func(_ string, nsp *Namespace) bool {
		nsp.Sockets().Range(func(_ SocketId, socket *Socket) bool {
			socket._onclose("server shutting down")
			return true
		})

		nsp.Adapter().Close()
		return true
	})

	if s.httpServer != nil {
		s.httpServer.Close(fn)
	} else {
		s.engine.Close()
		if fn != nil {
			fn(nil)
		}
	}
}

// Registers a middleware, which is a function that gets executed for every incoming [Socket].
//
//	io.Use(func(socket *socket.Socket, next func(*socket.ExtendedError)) {
//		// ...
//		next(nil)
//	})
//
// Param: func(*ExtendedError) - the middleware function
func (s *Server) Use(fn func(*Socket, func(*ExtendedError))) *Server {
	s.sockets.Use(fn)
	return s
}

// Targets a room when emitting.
//
//	// the “foo” event will be broadcast to all connected clients in the “room-101” room
//	io.To("room-101").Emit("foo", "bar")
//
//	// with an array of rooms (a client will be notified at most once)
//	io.To("room-101", "room-102").Emit("foo", "bar")
//	io.To([]Room{"room-101", "room-102"}...).Emit("foo", "bar")
//
//	// with multiple chained calls
//	io.To("room-101").To("room-102").Emit("foo", "bar")
//
// Param: Room - a [Room], or a [Room] slice to expand
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) To(room ...Room) *BroadcastOperator {
	return s.sockets.To(room...)
}

// Targets a room when emitting. Similar to `to()`, but might feel clearer in some cases:
//
//	// disconnect all clients in the "room-101" room
//	io.In("room-101").DisconnectSockets(false)
//
// Param: Room - a [Room], or a [Room] slice to expand
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) In(room ...Room) *BroadcastOperator {
	return s.sockets.In(room...)
}

// Excludes a room when emitting.
//
//	// the "foo" event will be broadcast to all connected clients, except the ones that are in the "room-101" room
//	io.Except("room-101").Emit("foo", "bar")
//
//	// with an array of rooms
//	io.Except(["room-101", "room-102"]).Emit("foo", "bar")
//	io.Except([]Room{"room-101", "room-102"}...).Emit("foo", "bar")
//
//	// with multiple chained calls
//	io.Except("room-101").Except("room-102").Emit("foo", "bar")
//
// Param: Room - a [Room], or a [Room] slice to expand
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) Except(room ...Room) *BroadcastOperator {
	return s.sockets.Except(room...)
}

// Sends a `message` event to all clients.
//
// This method mimics the WebSocket.send() method.
//
// See: https://developer.mozilla.org/en-US/docs/Web/API/WebSocket/send
//
//	io.Send("hello")
//
//	// this is equivalent to
//	io.Emit("message", "hello")
func (s *Server) Send(args ...any) *Server {
	// This type-cast is needed because EmitEvents likely doesn't have `message` as a key.
	// if you specify the EmitEvents, the type of args will be never.
	s.sockets.Emit("message", args...)
	return s
}

// Sends a `message` event to all clients. Alias of [Send].
func (s *Server) Write(args ...any) *Server {
	// This type-cast is needed because EmitEvents likely doesn't have `message` as a key.
	// if you specify the EmitEvents, the type of args will be never.
	s.sockets.Emit("message", args...)
	return s
}

// Sends a message to the other Socket.IO servers of the cluster.
//
//	io.ServerSideEmit("hello", "world")
//
//	io.On("hello", func(args ...any) {
//		fmt.Println(args) // prints "world"
//	})
//
//	// acknowledgements (without binary content) are supported too:
//	io.ServerSideEmit("ping", func(args []any, err error) {
//		if err != nil {
//			// some servers did not acknowledge the event in the given delay
//		} else {
//			fmt.Println(args) // one response per server (except the current one)
//		}
//	})
//
//	io.On("ping", func(args ...any) {
//		args[0]("pong")
//	})
//
// Param: ev - the event name
//
// Param: args - an slice of arguments, which may include an acknowledgement callback at the end
func (s *Server) ServerSideEmit(ev string, args ...any) error {
	return s.sockets.ServerSideEmit(ev, args...)
}

// Sends a message and expect an acknowledgement from the other Socket.IO servers of the cluster.
//
//	io.Timeout(1000 * time.Millisecond).ServerSideEmitWithAck("some-event")(func(args []any, err error) {
//		if err == nil {
//			fmt.Println(args) // one response per client
//		} else {
//			// some servers did not acknowledge the event in the given delay
//		}
//	})
//
// Param: ev - the event name
//
// Param: args - an array of arguments
//
// Return: a `func(func([]any, error))` that will be fulfilled when all servers have acknowledged the event
func (s *Server) ServerSideEmitWithAck(ev string, args ...any) func(func([]any, error)) {
	return s.sockets.ServerSideEmitWithAck(ev, args...)
}

// Gets a list of socket ids.
//
// Deprecated: this method will be removed in the next major release, please use [Server#ServerSideEmit] or [Server#FetchSockets] instead.
func (s *Server) AllSockets() (*types.Set[SocketId], error) {
	return s.sockets.AllSockets()
}

// Sets the compress flag.
//
//	io.Compress(false).Emit("hello")
//
// Param: bool - if `true`, compresses the sending data
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) Compress(compress bool) *BroadcastOperator {
	return s.sockets.Compress(compress)
}

// Sets a modifier for a subsequent event emission that the event data may be lost if the client is not ready to
// receive messages (because of network slowness or other issues, or because they’re connected through long polling
// and is in the middle of a request-response cycle).
//
//	io.Volatile().Emit("hello") // the clients may or may not receive it
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) Volatile() *BroadcastOperator {
	return s.sockets.Volatile()
}

// Sets a modifier for a subsequent event emission that the event data will only be broadcast to the current node.
//
//	// the “foo” event will be broadcast to all connected clients on this node
//	io.Local().Emit("foo", "bar")
//
// Return: a new [BroadcastOperator] instance for chaining
func (s *Server) Local() *BroadcastOperator {
	return s.sockets.Local()
}

// Adds a timeout in milliseconds for the next operation
//
//	io.Timeout(1000 * time.Millisecond).Emit("some-event", func(args []any, err error) {
//		if err != nil {
//			// some clients did not acknowledge the event in the given delay
//		} else {
//			fmt.Println(args) // one response per client
//		}
//	})
//
// Param: timeout
func (s *Server) Timeout(timeout time.Duration) *BroadcastOperator {
	return s.sockets.Timeout(timeout)
}

// Returns the matching socket instances
//
// Note: this method also works within a cluster of multiple Socket.IO servers, with a compatible [Adapter].
//
//	io.FetchSockets()(func(sockets []*RemoteSocket, _ error){
//		// return all Socket instances
//	})
//
//	// return all Socket instances in the "room1" room
//	io.In("room1").FetchSockets()(func(sockets []*RemoteSocket, _ error){
//
//		for _, socket := range sockets {
//			fmt.Println(socket.Id())
//			fmt.Println(socket.Handshake())
//			fmt.Println(socket.Rooms())
//			fmt.Println(socket.Data())
//
//			socket.Emit("hello")
//			socket.Join("room1")
//			socket.Leave("room2")
//			socket.Disconnect()
//		}
//
//	})
func (s *Server) FetchSockets() func(func([]*RemoteSocket, error)) {
	return s.sockets.FetchSockets()
}

// Makes the matching socket instances join the specified rooms
//
// Note: this method also works within a cluster of multiple Socket.IO servers, with a compatible [Adapter].
//
//	// make all socket instances join the "room1" room
//	io.SocketsJoin("room1")
//
//	// make all socket instances in the "room1" room join the "room2" and "room3" rooms
//	io.In("room1").SocketsJoin([]Room{"room2", "room3"}...)
//
// Param: Room - a [Room], or a [Room] slice to expand
func (s *Server) SocketsJoin(room ...Room) {
	s.sockets.SocketsJoin(room...)
}

// Makes the matching socket instances leave the specified rooms
//
// Note: this method also works within a cluster of multiple Socket.IO servers, with a compatible [Adapter].
//
//	// make all socket instances leave the "room1" room
//	io.SocketsLeave("room1")
//
//	// make all socket instances in the "room1" room leave the "room2" and "room3" rooms
//	io.In("room1").SocketsLeave([]Room{"room2", "room3"}...)
//
// Param: Room - a [Room], or a [Room] slice to expand
func (s *Server) SocketsLeave(room ...Room) {
	s.sockets.SocketsLeave(room...)
}

// Makes the matching socket instances disconnect
//
// Note: this method also works within a cluster of multiple Socket.IO servers, with a compatible [Adapter].
//
//	// make all socket instances disconnect (the connections might be kept alive for other namespaces)
//	io.DisconnectSockets(false)
//
//	// make all socket instances in the "room1" room disconnect and close the underlying connections
//	io.In("room1").DisconnectSockets(true)
//
// Param: close - whether to close the underlying connection
func (s *Server) DisconnectSockets(status bool) {
	s.sockets.DisconnectSockets(status)
}
