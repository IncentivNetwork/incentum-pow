// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package tracers

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/rpc"
)

func strPtr(s string) *string          { return &s }
func uintPtr(u uint64) *uint64         { return &u }
func callTracerConfig() *string        { return strPtr("callTracer") }
func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

func TestRestrictedDebugServiceExposesOnlyProfileMethods(t *testing.T) {
	service, err := newRestrictedDebugAPI(new(API), rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(service)

	var got []string
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		got = append(got, strings.ToLower(method.Name[:1])+method.Name[1:])
	}
	sort.Strings(got)

	want, err := rpc.DebugProfileMethods(rpc.DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restricted service exposes %v, want exactly %v", got, want)
	}
}

func TestRestrictedDebugServiceIgnoresForeignServices(t *testing.T) {
	// Another debug service must be left to the other factories rather than
	// mistaken for the tracing API.
	service, err := newRestrictedDebugAPI(struct{}{}, rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	if err != nil {
		t.Fatal(err)
	}
	if service != nil {
		t.Fatalf("foreign service restricted to %T", service)
	}
}

func TestRestrictedDebugServiceRejectsUnknownProfile(t *testing.T) {
	if _, err := newRestrictedDebugAPI(new(API), rpc.RestrictedDebugOptions{Profile: "trace-indexer-v2"}); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestValidateRestrictedTraceConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *TraceConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name:    "no tracer",
			config:  &TraceConfig{},
			wantErr: true,
		},
		{
			name:    "empty tracer",
			config:  &TraceConfig{Tracer: strPtr("")},
			wantErr: true,
		},
		{
			name:    "other native tracer",
			config:  &TraceConfig{Tracer: strPtr("prestateTracer")},
			wantErr: true,
		},
		{
			name:    "javascript tracer",
			config:  &TraceConfig{Tracer: strPtr("{step: function() {}, result: function() {}, fault: function() {}}")},
			wantErr: true,
		},
		{
			name:   "call tracer",
			config: &TraceConfig{Tracer: callTracerConfig()},
		},
		{
			name:   "onlyTopCall",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":true}`)},
		},
		{
			name:    "withLog",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"withLog":true}`)},
			wantErr: true,
		},
		{
			name:    "unknown tracer config field",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":true,"extra":1}`)},
			wantErr: true,
		},
		{
			name:    "wrong tracer config type",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":"yes"}`)},
			wantErr: true,
		},
		{
			name:    "null tracer config field",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":null}`)},
			wantErr: true,
		},
		{
			name:    "nested tracer config",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":{"a":1}}`)},
			wantErr: true,
		},
		{
			name:    "concatenated tracer config values",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":true}{"onlyTopCall":false}`)},
			wantErr: true,
		},
		{
			name:    "null tracer config",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`null`)},
			wantErr: true,
		},
		{
			name:    "array tracer config",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`[]`)},
			wantErr: true,
		},
		{
			name:    "primitive tracer config",
			config:  &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`true`)},
			wantErr: true,
		},
		{
			name:   "timeout within cap",
			config: &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("10s")},
		},
		{
			name:    "timeout over cap",
			config:  &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("10.001s")},
			wantErr: true,
		},
		{
			name:    "unparsable timeout",
			config:  &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("forever")},
			wantErr: true,
		},
		{
			name:    "negative timeout",
			config:  &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("-1s")},
			wantErr: true,
		},
		{
			name:   "reexec within cap",
			config: &TraceConfig{Tracer: callTracerConfig(), Reexec: uintPtr(128)},
		},
		{
			name:    "reexec over cap",
			config:  &TraceConfig{Tracer: callTracerConfig(), Reexec: uintPtr(129)},
			wantErr: true,
		},
		{
			name:   "zero struct logger config",
			config: &TraceConfig{Tracer: callTracerConfig(), Config: &logger.Config{}},
		},
		{
			name:    "struct logger memory capture",
			config:  &TraceConfig{Tracer: callTracerConfig(), Config: &logger.Config{EnableMemory: true}},
			wantErr: true,
		},
		{
			name:    "struct logger limit",
			config:  &TraceConfig{Tracer: callTracerConfig(), Config: &logger.Config{Limit: 1}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestrictedTraceConfig(tt.config)
			if tt.wantErr && err == nil {
				t.Fatal("config accepted, want rejection")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("config rejected: %v", err)
			}
			if err != nil {
				if code := err.(rpc.Error).ErrorCode(); code != -32602 {
					t.Fatalf("error code = %d, want -32602", code)
				}
			}
		})
	}
}

// TestValidateRestrictedTraceConfigMessages pins the text of every distinct
// rejection. The table above proves each configuration is refused; these
// assertions prove the caller is told which knob to turn. Every rejection is
// explicit rather than a silent clamp precisely so the message can be acted on,
// which only holds if the message says what was actually asked for.
func TestValidateRestrictedTraceConfigMessages(t *testing.T) {
	tests := []struct {
		name   string
		config *TraceConfig
		want   string
		prefix bool // match a prefix: the tail comes from encoding/json
	}{
		{
			name:   "nil config",
			config: nil,
			want:   `tracer is required on this transport and must be "callTracer"`,
		},
		{
			name:   "no tracer",
			config: &TraceConfig{},
			want:   `tracer must be "callTracer" on this transport`,
		},
		{
			name:   "other native tracer",
			config: &TraceConfig{Tracer: strPtr("prestateTracer")},
			want:   `tracer must be "callTracer" on this transport`,
		},
		{
			name:   "struct logger options",
			config: &TraceConfig{Tracer: callTracerConfig(), Config: &logger.Config{EnableMemory: true}},
			want:   "struct logger options are not available on this transport",
		},
		{
			name:   "unparsable timeout",
			config: &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("forever")},
			want:   `invalid timeout: time: invalid duration "forever"`,
		},
		{
			name:   "negative timeout",
			config: &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("-1s")},
			want:   "timeout -1s must be positive",
		},
		{
			// The message quotes the requested value, not the cap, so an
			// operator can see what the caller asked for.
			name:   "timeout over cap",
			config: &TraceConfig{Tracer: callTracerConfig(), Timeout: strPtr("10.001s")},
			want:   "timeout 10.001s exceeds the limit of 10s on this transport",
		},
		{
			name:   "reexec over cap",
			config: &TraceConfig{Tracer: callTracerConfig(), Reexec: uintPtr(129)},
			want:   "reexec 129 exceeds the limit of 128 on this transport",
		},
		{
			name:   "concatenated tracer config values",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":true}{"onlyTopCall":false}`)},
			want:   "invalid tracerConfig: invalid JSON",
		},
		{
			name:   "null tracer config",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`null`)},
			want:   "invalid tracerConfig: must be a JSON object",
		},
		{
			name:   "array tracer config",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`[]`)},
			want:   "invalid tracerConfig: must be a JSON object",
		},
		{
			name:   "withLog",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"withLog":true}`)},
			want:   `invalid tracerConfig: json: unknown field "withLog"`,
		},
		{
			name:   "null onlyTopCall",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":null}`)},
			want:   "invalid tracerConfig: onlyTopCall must be a boolean",
		},
		{
			name:   "wrong onlyTopCall type",
			config: &TraceConfig{Tracer: callTracerConfig(), TracerConfig: rawJSON(`{"onlyTopCall":"yes"}`)},
			want:   "invalid tracerConfig: json: cannot unmarshal string into Go struct field RestrictedCallTracerConfig.onlyTopCall",
			prefix: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestrictedTraceConfig(tt.config)
			if err == nil {
				t.Fatal("config accepted, want rejection")
			}
			if tt.prefix {
				if !strings.HasPrefix(err.Error(), tt.want) {
					t.Fatalf("error = %q, want prefix %q", err, tt.want)
				}
				return
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestRestrictedDebugAPIConcurrencyLimit(t *testing.T) {
	api := &RestrictedDebugAPI{
		limiter:        rpc.NewTraceLimiter(1),
		requestTimeout: time.Minute,
	}
	ctx, done, err := api.enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("restricted call has no whole-request deadline")
	}
	// A second concurrent call is refused rather than queued.
	if _, _, err := api.enter(context.Background()); err != rpc.ErrTooManyTraces {
		t.Fatalf("second enter = %v, want ErrTooManyTraces", err)
	}
	done()

	if _, done, err := api.enter(context.Background()); err != nil {
		t.Fatalf("enter after release: %v", err)
	} else {
		done()
	}
}

func TestRestrictedDebugAPIDefaultRequestTimeout(t *testing.T) {
	service, err := newRestrictedDebugAPI(new(API), rpc.RestrictedDebugOptions{Profile: rpc.DebugProfileTraceIndexerV1})
	if err != nil {
		t.Fatal(err)
	}
	if got := service.(*RestrictedDebugAPI).requestTimeout; got != restrictedDefaultRequestTimeout {
		t.Fatalf("request timeout = %s, want %s", got, restrictedDefaultRequestTimeout)
	}
}

// deadlineBackend blocks chain access until the actual delegated context ends.
// Embedding Backend makes any unexpected call fail instead of returning fake data.
type deadlineBackend struct {
	Backend
	entered chan context.Context
}

func (b *deadlineBackend) BlockByNumber(ctx context.Context, _ rpc.BlockNumber) (*types.Block, error) {
	b.entered <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *deadlineBackend) GetTransaction(ctx context.Context, _ common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	b.entered <- ctx
	<-ctx.Done()
	return nil, common.Hash{}, 0, 0, ctx.Err()
}

// Exercise both public wrappers, including delegation, cancellation and slot
// release. Waiting on enter() alone would still pass if either wrapper
// stopped using it or delegated with the original context.
func TestRestrictedDebugAPIRequestDeadlineAborts(t *testing.T) {
	const timeout = 50 * time.Millisecond
	for _, method := range []string{"transaction", "block"} {
		t.Run(method, func(t *testing.T) {
			backend := &deadlineBackend{entered: make(chan context.Context, 1)}
			limiter := rpc.NewTraceLimiter(1)
			service, err := newRestrictedDebugAPI(NewAPI(backend), rpc.RestrictedDebugOptions{
				Profile: rpc.DebugProfileTraceIndexerV1, Limiter: limiter, RequestTimeout: timeout,
			})
			if err != nil {
				t.Fatal(err)
			}
			api := service.(*RestrictedDebugAPI)
			caller, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			start := time.Now()
			go func() {
				config := &TraceConfig{Tracer: callTracerConfig()}
				var err error
				if method == "transaction" {
					_, err = api.TraceTransaction(caller, common.Hash{}, config)
				} else {
					_, err = api.TraceBlockByNumber(caller, rpc.LatestBlockNumber, config)
				}
				result <- err
			}()
			select {
			case ctx := <-backend.entered:
				deadline, ok := ctx.Deadline()
				if !ok || deadline.Sub(start) < timeout || time.Until(deadline) > timeout {
					t.Fatal("delegated context does not carry the request deadline")
				}
			case <-caller.Done():
				t.Fatal("request never reached the backend")
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("request error = %v, want DeadlineExceeded", err)
				}
				if caller.Err() != nil {
					t.Fatal("request only stopped at the caller's longer deadline")
				}
			case <-caller.Done():
				t.Fatal("request did not stop at the restricted deadline")
			}
			release, err := limiter.Acquire()
			if err != nil {
				t.Fatal("request leaked its concurrency slot:", err)
			}
			release()
		})
	}
}

// TestRestrictedDebugAPIRequestDeadlineTightensCaller covers the other
// direction: a caller that asks for longer than the bound does not get it.
func TestRestrictedDebugAPIRequestDeadlineTightensCaller(t *testing.T) {
	const timeout = 50 * time.Millisecond

	api := &RestrictedDebugAPI{
		limiter:        rpc.NewTraceLimiter(1),
		requestTimeout: timeout,
	}

	caller, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	ctx, done, err := api.enter(caller)
	if err != nil {
		t.Fatal(err)
	}
	defer done()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("no deadline attached to the request context")
	}
	if remaining := time.Until(deadline); remaining > timeout {
		t.Fatalf("caller's deadline survived: %s away, want at most %s", remaining, timeout)
	}
}
