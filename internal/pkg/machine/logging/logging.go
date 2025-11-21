// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package logging implements machine logging sink.
package logging

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/siderolabs/go-circular"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// LogEvent represents a log message to be send.
type LogEvent struct {
	Fields map[string]any
	Time   time.Time
	Msg    string
	Level  zapcore.Level
}

// ZapCore wrapper for forwarding log events to the siderolink logs endpoint.
type ZapCore struct {
	sender *LogSender
	buffer *circular.Buffer
	reader *circular.Reader
	fields []zap.Field
}

// NewZapCore creates a new zap core.
func NewZapCore(endpoint string) (*ZapCore, error) {
	e, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	buffer, err := circular.NewBuffer(circular.WithMaxCapacity(1048576))
	if err != nil {
		return nil, err
	}

	reader := buffer.GetReader()

	return &ZapCore{
		sender: NewLogSender(e),
		buffer: buffer,
		reader: reader,
	}, nil
}

// ConfigureInterface inits the sender.
func (core *ZapCore) ConfigureInterface(ctx context.Context, res *network.AddressStatus) error {
	core.sender.configure(res.TypedSpec().LinkName, res.TypedSpec().Address)

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	return core.flushBuffer(ctx)
}

// Close the sender.
func (core *ZapCore) Close(ctx context.Context) error {
	if err := core.reader.Close(); err != nil {
		return err
	}

	return core.sender.Close(ctx)
}

// With implements zapcore.Core interface.
func (core *ZapCore) With(fields []zapcore.Field) zapcore.Core {
	core.fields = fields

	return core
}

// Check implements zapcore.core interface.
func (core *ZapCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if core.Enabled(entry.Level) {
		return checked.AddCore(entry, core)
	}

	return checked
}

// Enabled implements zapcore.LevelEnabler interface.
func (core *ZapCore) Enabled(zapcore.Level) bool {
	return true
}

// Write implements zapcore.Core interface.
func (core *ZapCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Second)
	defer cancel()

	if err := core.flushBuffer(ctx); err != nil {
		return err
	}

	encoder := zapcore.NewMapObjectEncoder()

	for _, field := range fields {
		field.AddTo(encoder)
	}

	text := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{})

	context, err := text.EncodeEntry(entry, fields)
	if err != nil {
		return err
	}

	ev := &LogEvent{
		Msg:    entry.Message + " " + context.String(),
		Time:   entry.Time,
		Level:  entry.Level,
		Fields: encoder.Fields,
	}

	err = core.sender.Send(ctx, ev)
	if err != nil {
		d, err := json.Marshal(ev)
		if err != nil {
			return err
		}

		_, err = core.buffer.Write(d)

		return err
	}

	return nil
}

func (core *ZapCore) flushBuffer(ctx context.Context) error {
	if !core.sender.ready() {
		return nil
	}

	decoder := json.NewDecoder(core.reader)

	for {
		var ev LogEvent

		if err := decoder.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		if err := core.sender.Send(ctx, &ev); err != nil {
			return err
		}
	}
}

// Sync implements zapcore.Core interface.
func (core *ZapCore) Sync() error {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Second)
	defer cancel()

	return core.flushBuffer(ctx)
}
