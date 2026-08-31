package transfer

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Discover broadcasts a UDP query and collects receiver replies for up to wait
// (or until ctx's deadline, whichever is sooner).
func Discover(ctx context.Context, wait time.Duration) ([]ReceiverInfo, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: DiscoveryPort}
	if _, err := conn.WriteToUDP([]byte(discoverQuery), bcast); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(wait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetReadDeadline(deadline)

	seen := map[string]bool{}
	var out []ReceiverInfo
	buf := make([]byte, 1024)
	for {
		if ctx.Err() != nil {
			break
		}
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // deadline reached
		}
		fields := strings.Fields(string(buf[:n]))
		if len(fields) >= 3 && fields[0] == discoverReply {
			port := fields[len(fields)-1]
			name := CleanText(strings.Join(fields[1:len(fields)-1], " "))
			addr := fmt.Sprintf("%s:%s", from.IP.String(), port)
			if !seen[addr] {
				seen[addr] = true
				out = append(out, ReceiverInfo{Name: name, Addr: addr})
			}
		}
	}
	return out, nil
}

// Responder answers discovery queries until its context is cancelled or Close is
// called.
type Responder struct {
	conn *net.UDPConn
}

// StartResponder begins answering UDP discovery queries, advertising name and
// tcpPort. Discovery is a convenience; a direct send still works without it.
func StartResponder(ctx context.Context, name string, tcpPort int) (*Responder, error) {
	addr := net.UDPAddr{Port: DiscoveryPort, IP: net.IPv4zero}
	conn, err := net.ListenUDP("udp4", &addr)
	if err != nil {
		return nil, err
	}
	r := &Responder{conn: conn}
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	go r.loop(name, tcpPort)
	return r, nil
}

func (r *Responder) loop(name string, tcpPort int) {
	buf := make([]byte, 1024)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if strings.TrimSpace(string(buf[:n])) == discoverQuery {
			reply := fmt.Sprintf("%s %s %d", discoverReply, name, tcpPort)
			_, _ = r.conn.WriteToUDP([]byte(reply), from)
		}
	}
}

// Close stops the responder.
func (r *Responder) Close() error {
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
