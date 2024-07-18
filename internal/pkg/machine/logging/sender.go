// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/siderolabs/talemu/internal/pkg/machine/network"
)

// LogSender writes zap logs to the remote destination.
type LogSender struct {
	endpoint *url.URL
	conn     net.Conn
	sema     chan struct{}
	iface    string
	mu       sync.Mutex
}

// NewLogSender returns log sender that sends logs in JSON over TCP (newline-delimited)
// or UDP (one message per packet).
func NewLogSender(endpoint *url.URL) *LogSender {
	sema := make(chan struct{}, 1)
	sema <- struct{}{}

	return &LogSender{
		endpoint: endpoint,

		sema: sema,
	}
}

func (j *LogSender) ready() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.iface != ""
}

func (j *LogSender) configure(iface string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.iface = iface
}

func (j *LogSender) tryLock(ctx context.Context) (unlock func()) {
	select {
	case <-j.sema:
		unlock = func() { j.sema <- struct{}{} }
	case <-ctx.Done():
		unlock = nil
	}

	return
}

func (j *LogSender) marshalJSON(e *LogEvent) ([]byte, error) {
	m := make(map[string]interface{}, len(e.Fields)+3)
	for k, v := range e.Fields {
		m[k] = v
	}

	m["msg"] = e.Msg
	m["talos-time"] = e.Time.Format(time.RFC3339Nano)
	m["talos-level"] = e.Level.String()

	return json.Marshal(m)
}

// Send implements LogSender interface.
func (j *LogSender) Send(ctx context.Context, e *LogEvent) error {
	j.mu.Lock()
	iface := j.iface
	j.mu.Unlock()

	if iface == "" {
		return fmt.Errorf("the log sender is not ready yet")
	}

	b, err := j.marshalJSON(e)
	if err != nil {
		return err
	}

	if j.endpoint.Scheme == "tcp" {
		b = append(b, '\n')
	}

	unlock := j.tryLock(ctx)
	if unlock == nil {
		return ctx.Err()
	}

	defer unlock()

	var dialer net.Dialer

	dialer.Control = network.BindToInterface(iface)

	// Connect (or "connect" for UDP) if no connection is established already.
	if j.conn == nil {
		conn, err := dialer.DialContext(ctx, j.endpoint.Scheme, j.endpoint.Host)
		if err != nil {
			return err
		}

		j.conn = conn
	}

	d, _ := ctx.Deadline()
	j.conn.SetWriteDeadline(d) //nolint:errcheck

	// Close connection on send error.
	if _, err := j.conn.Write(b); err != nil {
		j.conn.Close() //nolint:errcheck
		j.conn = nil

		return err
	}

	return nil
}

// Close implements LogSender interface.
func (j *LogSender) Close(ctx context.Context) error {
	unlock := j.tryLock(ctx)
	if unlock == nil {
		return ctx.Err()
	}

	defer unlock()

	if j.conn == nil {
		return nil
	}

	conn := j.conn
	j.conn = nil

	closed := make(chan error, 1)

	go func() {
		closed <- conn.Close()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-closed:
		return err
	}
}
