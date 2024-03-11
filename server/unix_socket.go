package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

type UnixSocketServer struct {
	socketPath string
	listener   net.Listener

	mu                  sync.Mutex
	commands            []string
	commandRecordedHook func(string)

	closed atomic.Bool
}

var ErrStartingRecordingSession = errors.New("failed to start recording session")

const defaultSocketPath = "/tmp/savvy-socket"

type Option func(*UnixSocketServer)

func WithCommandRecordedHook(hook func(string)) Option {
	return func(s *UnixSocketServer) {
		s.commandRecordedHook = hook
	}
}

func NewUnixSocketServerWithDefaultPath(opts ...Option) (*UnixSocketServer, error) {
	return NewUnixSocketServer(defaultSocketPath, opts...)
}

func NewUnixSocketServer(socketPath string, opts ...Option) (*UnixSocketServer, error) {
	if fileInfo, _ := os.Stat(socketPath); fileInfo != nil {
		return nil, fmt.Errorf("%w: concurrent recording sessions are not supported yet", ErrStartingRecordingSession)
	}
	return newUnixSocketServer(socketPath, opts...)
}

func newUnixSocketServer(socketPath string, opts ...Option) (*UnixSocketServer, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	srv := &UnixSocketServer{
		socketPath: socketPath,
		listener:   listener,
	}

	for _, opt := range opts {
		opt(srv)
	}

	return srv, nil
}

func (s *UnixSocketServer) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.commands
}

func (s *UnixSocketServer) Close() error {
	if s.listener != nil {
		s.closed.Store(true)
		return s.listener.Close()
	}
	return nil
}

func (s *UnixSocketServer) ListenAndServe() {
	for {
		// Accept new connections
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			slog.Debug("Failed to accept connection:", "error", err.Error())
			continue
		}

		// Handle the connection
		go s.handleConnection(conn)
	}
}

func (s *UnixSocketServer) handleConnection(c net.Conn) {
	defer c.Close()

	bs, err := io.ReadAll(c)
	if err != nil {
		fmt.Printf("Failed to read from connection: %s\n", err)
		return
	}
	command := string(bs)
	s.appendCommand(command)
	if s.commandRecordedHook != nil {
		s.commandRecordedHook(command)
	}
}

func (s *UnixSocketServer) SocketPath() string {
	return s.socketPath
}

func (s *UnixSocketServer) appendCommand(command string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.commands = append(s.commands, command)
}
