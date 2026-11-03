// Package instance lets one onon process hand work to another that is already
// running: opening a document in the existing window instead of a new one,
// moving to a page or a point on it, and running a viewer command there.
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrInUse reports that another live instance already owns the address.
var ErrInUse = errors.New("another onon instance is already listening")

// dialTimeout bounds both the liveness probe and a client handoff. It is short
// because the peer is a local process that either answers at once or is gone.
const dialTimeout = 2 * time.Second

// replyTimeout bounds how long a connection waits for the viewer to act. The
// viewer answers from its event loop, so a busy render can delay it, but a
// wedged one must not hold the client forever.
const replyTimeout = 10 * time.Second

// Request is one command for a running instance.
type Request struct {
	// Command is "open", "open-tab", "open-document", "run", or "ping".
	// "open-tab" goes to a window's address, to open Path in a new tab
	// there, or with no Path an empty one; "open-document" opens it as a
	// document the system sends is.
	Command string `json:"command"`
	// Path is the document to open, absolute.
	Path string `json:"path,omitempty"`
	// Page is a 1-based page to move to, or 0 for none.
	Page int `json:"page,omitempty"`
	// X and Y are a document position on Page, used only when HasPoint is set.
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	HasPoint bool    `json:"has_point,omitempty"`
	// Text is the viewer command line for the "run" command, without its
	// leading colon.
	Text string `json:"text,omitempty"`
}

// Response is what the running instance reports back.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Delivery pairs a request with the channel its reply must go to. The viewer
// handles the request on its own thread and then calls Reply.
type Delivery struct {
	Request Request
	reply   chan Response
}

// Reply answers the waiting client. It is safe to call at most once; later
// calls and calls after the client gave up are discarded.
func (d Delivery) Reply(err error) {
	response := Response{OK: err == nil}
	if err != nil {
		response.Error = err.Error()
	}
	select {
	case d.reply <- response:
	default:
	}
}

// Server accepts commands for this process.
type Server struct {
	listener   net.Listener
	address    string
	deliveries chan Delivery
	closed     chan struct{}
}

// Listen claims the address for this process. A socket left behind by a crashed
// instance is removed and reclaimed; one that still answers yields ErrInUse.
func Listen(address string) (*Server, error) {
	if address == "" {
		return nil, errors.New("empty instance address")
	}
	listener, err := net.Listen("unix", address)
	if err != nil {
		if !isAddrInUse(err) {
			return nil, err
		}
		// Something holds the path. Only a live peer may keep it.
		if _, probeErr := Send(address, Request{Command: "ping"}); probeErr == nil {
			return nil, ErrInUse
		}
		if removeErr := os.Remove(address); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("remove stale socket: %w", removeErr)
		}
		listener, err = net.Listen("unix", address)
		if err != nil {
			return nil, err
		}
	}
	// The socket carries commands that act on this user's session, so keep it
	// readable only by its owner even when the directory is shared.
	if err := os.Chmod(address, 0o600); err != nil && !os.IsNotExist(err) {
		listener.Close()
		return nil, err
	}
	server := &Server{
		listener:   listener,
		address:    address,
		deliveries: make(chan Delivery, 16),
		closed:     make(chan struct{}),
	}
	go server.accept()
	return server, nil
}

// Deliveries yields commands received from other processes. Each must be
// answered with Reply.
func (s *Server) Deliveries() <-chan Delivery { return s.deliveries }

// Address is the path this server listens on.
func (s *Server) Address() string { return s.address }

// Close stops listening and removes the socket.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	select {
	case <-s.closed:
		return nil
	default:
		close(s.closed)
	}
	err := s.listener.Close()
	os.Remove(s.address)
	return err
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			// A transient accept error should not kill single-instance support
			// for the rest of the session.
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(replyTimeout + dialTimeout))

	var request Request
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		json.NewEncoder(conn).Encode(Response{Error: "malformed request"})
		return
	}
	// Ping is answered here so liveness never depends on the viewer's thread;
	// a wedged window must still count as "in use".
	if request.Command == "ping" {
		json.NewEncoder(conn).Encode(Response{OK: true})
		return
	}

	reply := make(chan Response, 1)
	select {
	case s.deliveries <- Delivery{Request: request, reply: reply}:
	case <-s.closed:
		json.NewEncoder(conn).Encode(Response{Error: "instance is shutting down"})
		return
	case <-time.After(replyTimeout):
		json.NewEncoder(conn).Encode(Response{Error: "instance is not accepting commands"})
		return
	}

	select {
	case response := <-reply:
		json.NewEncoder(conn).Encode(response)
	case <-s.closed:
		json.NewEncoder(conn).Encode(Response{Error: "instance is shutting down"})
	case <-time.After(replyTimeout):
		json.NewEncoder(conn).Encode(Response{Error: "instance did not respond"})
	}
}

// Send hands a request to the instance listening on address. It returns an
// error when nothing is listening, so callers can treat that as "no instance".
func Send(address string, request Request) (Response, error) {
	if address == "" {
		return Response{}, errors.New("empty instance address")
	}
	conn, err := net.DialTimeout("unix", address, dialTimeout)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(replyTimeout + dialTimeout))

	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if !response.OK {
		message := response.Error
		if message == "" {
			message = "instance rejected the request"
		}
		return response, errors.New(message)
	}
	return response, nil
}

// ParseTarget reads a --goto specification: "PAGE" for a page, or "PAGE:X:Y"
// for a point on it. X and Y are points from the page's top-left corner. An
// empty specification means no jump was asked for.
func ParseTarget(spec string) (*Request, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	fields := strings.Split(spec, ":")
	page, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil || page < 1 {
		return nil, fmt.Errorf("--goto: %q does not start with a 1-based page", spec)
	}
	request := &Request{Page: page}
	switch len(fields) {
	case 1:
		return request, nil
	case 3:
		x, xErr := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
		y, yErr := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		if xErr != nil || yErr != nil {
			return nil, fmt.Errorf("--goto: %q has an unreadable coordinate", spec)
		}
		request.X, request.Y, request.HasPoint = x, y, true
		return request, nil
	default:
		return nil, fmt.Errorf("--goto: want PAGE or PAGE:X:Y, got %q", spec)
	}
}
