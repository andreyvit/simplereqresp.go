package simplereqresp

import (
	"fmt"
	"time"
)

const addr = "127.0.0.1:7123"

var replies = map[string]string{
	"foo": "bar",
	"boz": "fubar",
}

func Example_simple() {
	var cl *Client
	var srv *Server

	srv, err := Listen(addr, ServerOptions{
		Verbosity: VeryVerbose,
		RequestHandler: func(input []byte) ([]byte, error) {
			fmt.Printf("Request: %q\n", input)
			return []byte(replies[string(input)] + "\n"), nil
		},
		ConnectionHandler: func(c *ServerConn) error {
			fmt.Printf("Client connected.\n")
			defer fmt.Printf("Client disconnected.\n")
			return srv.HandleRequests(c)
		},
	})
	if err != nil {
		panic(err)
	}
	go srv.MustServe()

	cl = Dial(addr, ClientOptions{
		Verbosity:         VeryVerbose,
		ConnectTimeout:    500 * time.Millisecond,
		ReconnectionDelay: 500 * time.Millisecond,
		SendTimeout:       500 * time.Millisecond,
		ResponseTimeout:   1 * time.Second,
		RequestTimeout:    2 * time.Second,
	})

	resp, err := cl.RequestString("foo\n")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Response: %q\n", resp)

	resp, err = cl.RequestString("boz\n")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Response: %q\n", resp)
	cl.Shutdown()

	cl.Wait()
	srv.ShutdownWait()

	// Output: Client connected.
	// Request: "foo"
	// Response: "bar"
	// Request: "boz"
	// Response: "fubar"
	// Client disconnected.
}

func Example_client_then_server() {
	var cl *Client
	var srv *Server

	cl = Dial(addr, ClientOptions{
		Verbosity:         VeryVerbose,
		ConnectTimeout:    300 * time.Millisecond,
		ReconnectionDelay: 300 * time.Millisecond,
		SendTimeout:       500 * time.Millisecond,
		ResponseTimeout:   1 * time.Second,
		RequestTimeout:    2 * time.Second,
	})

	go func() {
		resp, err := cl.RequestString("foo\n")
		if err != nil {
			panic(err)
		}
		fmt.Printf("Response: %q\n", resp)

		resp, err = cl.RequestString("boz\n")
		if err != nil {
			panic(err)
		}
		fmt.Printf("Response: %q\n", resp)

		cl.Shutdown()
	}()

	go func() {
		time.Sleep(1 * time.Second)
		fmt.Println("Starting server")

		var err error
		srv, err = Listen(addr, ServerOptions{
			Verbosity: VeryVerbose,
			RequestHandler: func(input []byte) ([]byte, error) {
				fmt.Printf("Request: %q\n", input)
				return []byte(replies[string(input)] + "\n"), nil
			},
			ConnectionHandler: func(c *ServerConn) error {
				fmt.Printf("Client connected.\n")
				defer fmt.Printf("Client disconnected.\n")
				return srv.HandleRequests(c)
			},
		})
		if err != nil {
			panic(err)
		}
		srv.MustServe()
	}()

	cl.Wait()
	srv.ShutdownWait()

	// Output: Starting server
	// Client connected.
	// Request: "foo"
	// Response: "bar"
	// Request: "boz"
	// Response: "fubar"
	// Client disconnected.
}
