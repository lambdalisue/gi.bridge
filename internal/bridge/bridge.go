package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/lambdalisue/gi.bridge/internal/pkg/ctxio"
)

const (
	delim            = ":"
	prefixAddress    = "a"
	prefixConnect    = "c"
	prefixDisconnect = "d"
	prefixReceive    = "r"
)

type bridge struct {
	in       io.ReadCloser
	out      io.WriteCloser
	listener net.Listener
	connMap  map[string]net.Conn
}

func New(in io.ReadCloser, out io.WriteCloser) *bridge {
	return &bridge{
		in:      in,
		out:     out,
		connMap: make(map[string]net.Conn),
	}
}

func (b *bridge) Start(ctx context.Context, addr string) error {
	g, ctx := errgroup.WithContext(ctx)

	// Listen
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen TCP on %s: %w", addr, err)
	}
	b.listener = l

	// Notify address
	w := bufio.NewWriter(ctxio.Writer(ctx, b.out))
	if _, err := w.WriteString(fmt.Sprintf("%s:%s\n", prefixAddress, l.Addr())); err != nil {
		return fmt.Errorf("failed to write listen address %s: %w", l.Addr(), err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	// Start handlers
	g.Go(func() error {
		return b.handleIncoming(ctx)
	})
	g.Go(func() error {
		return b.handleAccept(ctx, g)
	})
	return g.Wait()
}

func (b *bridge) handleIncoming(ctx context.Context) error {
	r := bufio.NewReader(ctxio.Reader(ctx, b.in))
	for {
		data, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				// XXX: Are you sure?
				return nil
			}
			return fmt.Errorf("error: failed to read incoming data: %w", err)
		}
		text := strings.TrimSpace(data)
		// Find which port
		m := strings.SplitN(text, delim, 2)
		if len(m) != 2 {
			log.Printf("warn: the incoming data does not follow the syntax (port:expr): %s", text)
			continue
		}
		port := m[0]
		expr := m[1]
		conn, ok := b.connMap[port]
		if !ok {
			log.Printf("warn: no connection exists for %s", port)
			continue
		}
		if _, err := conn.Write([]byte(expr)); err != nil {
			log.Printf("warn: failed to write data %s to %s: %s", expr, port, err)
			continue
		}
	}
}

func (b *bridge) handleAccept(ctx context.Context, g *errgroup.Group) error {
	if b.listener == nil {
		return fmt.Errorf("'listener' is nil and handleAccept must be called after proper initialization")
	}
	listener := b.listener
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok {
				if ne.Temporary() {
					continue
				}
			}
			return fmt.Errorf("failed to accept connection by non tempoary error: %w", err)
		}
		// Register conn
		_, port, err := net.SplitHostPort(conn.RemoteAddr().String())
		if err != nil {
			return fmt.Errorf("failed to split remote addr: %s: %w", conn.RemoteAddr(), err)
		}
		b.connMap[port] = conn
		// Notify port
		w := bufio.NewWriter(ctxio.Writer(ctx, b.out))
		if _, err := w.WriteString(fmt.Sprintf("%s:%s\n", prefixConnect, port)); err != nil {
			return fmt.Errorf("failed to write connected remote port %s: %w", port, err)
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush: %w", err)
		}
		// Start handling outgoing messages
		g.Go(func() error {
			return b.handleOutgoing(ctx, port, conn)
		})
	}
}

func (b *bridge) handleOutgoing(ctx context.Context, port string, conn net.Conn) error {
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(ctxio.Writer(ctx, b.out))
	defer func() {
		if _, err := w.WriteString(fmt.Sprintf("%s:%s\n", prefixDisconnect, port)); err != nil {
			log.Printf("warn: failed to write disconnection from %s: %s", conn, err)
		}
		if err := w.Flush(); err != nil {
			log.Printf("error: failed to flush: %s", err)
		}
		delete(b.connMap, port)
		conn.Close()
	}()
	for {
		data, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF || err == io.ErrClosedPipe {
				// XXX: Are you sure?
				return nil
			}
			return fmt.Errorf("error: failed to read outgoing data: %w", err)
		}
		text := strings.TrimSpace(data)
		if _, err := w.WriteString(fmt.Sprintf("%s:%s:%s\n", prefixReceive, port, text)); err != nil {
			log.Printf("warn: failed to write data %s from %s: %s", data, conn, err)
			continue
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("error: failed to flush: %w", err)
		}
	}
}
