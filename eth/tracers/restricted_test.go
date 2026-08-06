// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package tracers

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

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
