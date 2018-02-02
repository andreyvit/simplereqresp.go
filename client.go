package simplereqresp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

var (
	ErrNoResponse = errors.New("server closed connection without sending a response")
)

type ClientOptions struct {
	ConnectTimeout    time.Duration
	ReconnectionDelay time.Duration

	// keep retrying a request this long
	RequestTimeout time.Duration

	// Time to actually send the request
	SendTimeout time.Duration

	// Wait for response this long
	ResponseTimeout time.Duration

	// KeepAliveTimeout  time.Duration

	MaxMessageSize int

	DebugName string

	Verbosity int
	Logger    Logger

	Splitter bufio.SplitFunc
}

type Client struct {
	Addr string
	ClientOptions

	once sync.Once
	wg   sync.WaitGroup

	reqc chan request

	cancel context.CancelFunc
}

type request struct {
	input []byte
	respc chan response
}

type response struct {
	output []byte
	err    error
}

func Dial(addr string, opt ClientOptions) *Client {
	if opt.ConnectTimeout == 0 {
		opt.ConnectTimeout = 5 * time.Second
	}
	if opt.ReconnectionDelay == 0 {
		opt.ReconnectionDelay = 1 * time.Second
	}
	if opt.SendTimeout == 0 {
		opt.SendTimeout = 10 * time.Second
	}
	if opt.ResponseTimeout == 0 {
		opt.ResponseTimeout = 1 * time.Minute
	}
	if opt.RequestTimeout == 0 {
		opt.RequestTimeout = (opt.SendTimeout+opt.ResponseTimeout)*2 + 1*time.Minute
	}

	if opt.Splitter == nil {
		opt.Splitter = ScanFullLines
	}

	ctx, cancel := context.WithCancel(context.Background())

	cl := &Client{
		Addr:          addr,
		ClientOptions: opt,

		reqc: make(chan request),

		cancel: cancel,
	}
	if cl.DebugName == "" {
		cl.DebugName = "simplelineprotocol.Client"
	}
	if cl.Logger == nil {
		cl.Logger = stdLogger{}
	}
	cl.wg.Add(1)
	go cl.run(ctx)
	return cl
}

func (cl *Client) Shutdown() {
	cl.cancel()
}

func (cl *Client) Wait() {
	cl.wg.Wait()
}

func (cl *Client) ShutdownWait() {
	cl.Shutdown()
	cl.Wait()
}

func (cl *Client) String() string {
	return cl.DebugName
}

func (cl *Client) RequestString(input string) (string, error) {
	r, err := cl.Request([]byte(input))
	if r != nil {
		return string(r), err
	} else {
		return "", err
	}
}

func (cl *Client) Request(input []byte) ([]byte, error) {
	respc := make(chan response)
	cl.reqc <- request{input, respc}
	resp := <-respc
	return resp.output, resp.err
}

func (cl *Client) run(ctx context.Context) {
	var conn clientConnectionState
	var reqid int

	defer cl.wg.Done()
	defer conn.close()

	donec := ctx.Done()

	if cl.Verbosity >= VeryVerbose {
		cl.Logger.Printf("%s: started", cl.DebugName)
	}
	for {
		select {
		case <-donec:
			if cl.Verbosity >= VeryVerbose {
				cl.Logger.Printf("%s: shutting down", cl.DebugName)
			}
			return
		case req := <-cl.reqc:
			reqid++
			reqStart := time.Now()
			deadline := reqStart.Add(cl.RequestTimeout)
			if cl.Verbosity >= VeryVerbose {
				cl.Logger.Printf("%s: req %d: sending: %s", cl.DebugName, reqid, req.input)
			} else if cl.Verbosity >= Verbose {
				cl.Logger.Printf("%s: req %d: sending", cl.DebugName, reqid)
			}

			var attempt int
		retryLoop:
			for {
				attempt++
				output, err := cl.doRequest(ctx, req.input, &conn, deadline)
				if err != nil {
					conn.close()
				}

				if err == nil || time.Now().After(deadline) {
					req.respc <- response{output, err}
					if err != nil {
						if cl.Verbosity >= Helpful {
							cl.Logger.Printf("ERROR: %s: req %d: permanent error: %v", cl.DebugName, reqid, err)
						}
					}
					break retryLoop
				}

				if cl.Verbosity >= Helpful {
					cl.Logger.Printf("WARNING: %s: req %d: temporary error: %v", cl.DebugName, reqid, err)
				}

				if attempt > 1 {
					delay := time.Until(deadline)
					if delay > cl.ReconnectionDelay {
						delay = cl.ReconnectionDelay
					}
					if cl.Verbosity >= VeryVerbose {
						cl.Logger.Printf("%s: req %d: will retry after %d ms", cl.DebugName, reqid, delay/time.Millisecond)
					}
					select {
					case <-time.After(delay):
						if cl.Verbosity >= Verbose {
							cl.Logger.Printf("%s: req %d: retrying, attempt %d", cl.DebugName, reqid, attempt+1)
						}
					case <-donec:
						if cl.Verbosity >= VeryVerbose {
							cl.Logger.Printf("%s: req %d: shutdown request while waiting for retry", cl.DebugName, reqid)
						}
						// will actually stop on the next connection attempt
					}
				}
			}
		}
	}
}

func (cl *Client) doRequest(ctx context.Context, input []byte, conn *clientConnectionState, deadline time.Time) ([]byte, error) {
	err := ctx.Err()
	if err != nil {
		return nil, err
	}

	if conn.netconn == nil {
		dialer := &net.Dialer{Deadline: deadline, Timeout: cl.ConnectTimeout}
		conn.netconn, err = dialer.DialContext(ctx, "tcp", cl.Addr)
		if err != nil {
			return nil, err
		}

		conn.scanner = bufio.NewScanner(conn.netconn)
		conn.scanner.Split(cl.Splitter)
		if cl.MaxMessageSize > 0 {
			conn.scanner.Buffer(make([]byte, 0, 64*1024), cl.MaxMessageSize)
		}
		conn.id++
	}

	err = conn.netconn.SetWriteDeadline(time.Now().Add(cl.SendTimeout))
	if err != nil {
		return nil, err
	}

	_, err = conn.netconn.Write(input)
	if err != nil {
		return nil, err
	}

	err = conn.netconn.SetReadDeadline(time.Now().Add(cl.ResponseTimeout))
	if err != nil {
		return nil, err
	}

	if !conn.scanner.Scan() {
		err = conn.scanner.Err()
		if err == nil {
			err = ErrNoResponse
		}
		return nil, err
	}

	return conn.scanner.Bytes(), nil
}

type clientConnectionState struct {
	id      uint64
	netconn net.Conn
	scanner *bufio.Scanner
}

func (conn *clientConnectionState) close() {
	if conn.netconn != nil {
		err := conn.netconn.Close()
		if err != nil {
			panic(err)
		}
		conn.netconn = nil
		conn.scanner = nil
	}
}
