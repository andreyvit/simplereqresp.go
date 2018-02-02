package simplereqresp

import (
	"bufio"
	"fmt"
	"net"
	"sync"
)

type ServerOptions struct {
	Splitter bufio.SplitFunc

	ConnectionHandler func(c *ServerConn) error

	RequestHandler func(input []byte) ([]byte, error)

	ErrHandler func(c *ServerConn, err error)

	DebugName string
	Verbosity int
	Logger    Logger
}

func Listen(addr string, opt ServerOptions) (*Server, error) {
	if opt.ConnectionHandler == nil && opt.RequestHandler == nil {
		panic("Server must have ConnectionHandler or RequestHandler set")
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		ServerOptions: opt,
		listener:      l.(*net.TCPListener),
		shutdownc:     make(chan struct{}),
	}
	if srv.ConnectionHandler == nil {
		srv.ConnectionHandler = srv.HandleRequests
	}
	if srv.ErrHandler == nil {
		srv.ErrHandler = srv.HandleError
	}
	if srv.Splitter == nil {
		srv.Splitter = ScanFullLines
	}
	if srv.DebugName == "" {
		srv.DebugName = "simplelineprotocol.Server" //fmt.Sprintf("simplelineprotocol.Server<%v>", srv.listener.Addr())
	}
	if srv.Logger == nil {
		srv.Logger = stdLogger{}
	}
	return srv, nil
}

type Server struct {
	ServerOptions

	listener *net.TCPListener

	connId uint64

	wg sync.WaitGroup

	shutdownc   chan struct{}
	shutdownmut sync.Mutex
}

func (srv *Server) String() string {
	return srv.DebugName
}

func (srv *Server) Addr() *net.TCPAddr {
	return srv.listener.Addr().(*net.TCPAddr)
}

func (srv *Server) Serve() error {
	srv.wg.Add(1)
	defer srv.wg.Done()
	defer srv.listener.Close()

	if srv.Verbosity >= VeryVerbose {
		srv.Logger.Printf("%v: serving", srv)
		defer srv.Logger.Printf("%v: shutting down", srv)
	}

	for {
		conn, err := srv.listener.Accept()
		if err != nil {
			select {
			case <-srv.shutdownc:
				return nil
			}
			return err
		}

		srv.connId++
		srv.wg.Add(1)
		go srv.handleConnection(conn, srv.connId)
	}
}

func (srv *Server) MustServe() {
	err := srv.Serve()
	if err != nil {
		panic(err)
	}
}

func (srv *Server) Shutdown() {
	// close, but exactly once
	srv.shutdownmut.Lock()
	select {
	case <-srv.shutdownc:
	default:
		close(srv.shutdownc)
		srv.listener.Close()
	}
	srv.shutdownmut.Unlock()
}

func (srv *Server) Wait() {
	srv.wg.Wait()
}

func (srv *Server) ShutdownWait() {
	srv.Shutdown()
	srv.Wait()
}

func (srv *Server) handleConnection(conn net.Conn, id uint64) {
	defer srv.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Split(srv.Splitter)
	c := &ServerConn{id, conn, scanner}

	if srv.Verbosity >= VeryVerbose {
		srv.Logger.Printf("%v %v: connected", srv, c)
	}

	err := srv.ConnectionHandler(c)
	if err != nil {
		srv.ErrHandler(c, err)
	}
}

func (srv *Server) HandleRequests(c *ServerConn) error {
	var reqid uint64
	for {
		inp, err := c.Recv()
		if err != nil {
			return err
		}
		if inp == nil {
			if srv.Verbosity >= VeryVerbose {
				srv.Logger.Printf("%v %v: EOF", srv, c)
			}
			return nil
		}

		reqid++
		if srv.Verbosity >= VeryVerbose {
			srv.Logger.Printf("%v %v: handling request %d: %s", srv, c, reqid, inp)
		} else if srv.Verbosity >= Verbose {
			srv.Logger.Printf("%v %v: handling request %d", srv, c, reqid)
		}

		outp, err := srv.RequestHandler(inp)
		if err != nil {
			return err
		}
		if outp != nil {
			err = c.Send(outp)
			if err != nil {
				return err
			}
		}
	}
}

func (srv *Server) HandleError(c *ServerConn, err error) {
	srv.Logger.Printf("WARNING: %v %v: %+v", srv, c, err)
}

type ServerConn struct {
	id      uint64
	conn    net.Conn
	scanner *bufio.Scanner
}

func (c *ServerConn) ConnID() uint64 {
	return c.id
}

func (c *ServerConn) String() string {
	return fmt.Sprintf("C_%d", c.id)
}

func (c *ServerConn) Recv() ([]byte, error) {
	if c.scanner.Scan() {
		return c.scanner.Bytes(), nil
	} else {
		return nil, c.scanner.Err()
	}
}

func (c *ServerConn) Send(input []byte) error {
	_, err := c.conn.Write(input)
	return err
}

func (c *ServerConn) RoundTripString(input string) (string, error) {
	r, err := c.RoundTrip([]byte(input))
	if r != nil {
		return string(r), err
	} else {
		return "", err
	}
}

func (c *ServerConn) RoundTrip(input []byte) ([]byte, error) {
	err := c.Send(input)
	if err != nil {
		return nil, err
	}
	return c.Recv()
}
