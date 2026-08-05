// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package tracers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	// restrictedTracer is the only tracer a restricted transport may select.
	// Leaving the tracer unset falls back to the struct logger, which can emit
	// a response several orders of magnitude larger than a call trace, so the
	// field is required rather than defaulted.
	restrictedTracer = "callTracer"

	// restrictedMaxTraceTimeout bounds the per-transaction trace timeout a
	// caller may ask for.
	restrictedMaxTraceTimeout = 10 * time.Second

	// restrictedMaxReexec bounds how far back the node may replay to rebuild
	// the state a trace starts from. 128 is the geth default.
	restrictedMaxReexec = 128

	// restrictedDefaultRequestTimeout bounds a whole restricted request when
	// the transport does not specify one. Tracing a block sums the per-
	// transaction timeout over every transaction in it, so the per-transaction
	// bound above does not bound the request.
	restrictedDefaultRequestTimeout = 30 * time.Second
)

// RestrictedCallTracerConfig is the only tracer configuration accepted on a
// restricted transport. It is strict-decoded: any other field, including
// withLog, is rejected.
type RestrictedCallTracerConfig struct {
	OnlyTopCall bool `json:"onlyTopCall"`
}

// RestrictedDebugAPI is a capability-restricted view of API for untrusted
// transports. It exposes only the methods of a debug profile, validates their
// arguments, bounds their concurrency and bounds their wall-clock duration,
// then delegates to the unrestricted API.
//
// The unrestricted API is deliberately left untouched: IPC and other trusted
// paths keep the full debug surface.
type RestrictedDebugAPI struct {
	api            *API
	limiter        *rpc.TraceLimiter
	requestTimeout time.Duration
}

func init() {
	rpc.RegisterRestrictedDebugFactory(newRestrictedDebugAPI)
}

// newRestrictedDebugAPI builds the restricted view of the tracing API. It
// returns a nil service for any other debug service, leaving it to the other
// registered factories — and, if none handles it, to being dropped from the
// restricted transport entirely.
func newRestrictedDebugAPI(service interface{}, opts rpc.RestrictedDebugOptions) (interface{}, error) {
	api, ok := service.(*API)
	if !ok {
		return nil, nil
	}
	if _, err := rpc.DebugProfileMethods(opts.Profile); err != nil {
		return nil, err
	}
	if opts.Profile != rpc.DebugProfileTraceIndexerV1 {
		return nil, fmt.Errorf("debug profile %q is not served by the tracing API", opts.Profile)
	}
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = restrictedDefaultRequestTimeout
	}
	return &RestrictedDebugAPI{api: api, limiter: opts.Limiter, requestTimeout: timeout}, nil
}

// TraceTransaction traces a single mined transaction with a call tracer.
func (api *RestrictedDebugAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *TraceConfig) (interface{}, error) {
	if err := validateRestrictedTraceConfig(config); err != nil {
		return nil, err
	}
	ctx, done, err := api.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	return api.api.TraceTransaction(ctx, hash, config)
}

// TraceBlockByNumber traces every transaction of a canonical block with a call
// tracer.
func (api *RestrictedDebugAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *TraceConfig) ([]*txTraceResult, error) {
	if err := validateRestrictedTraceConfig(config); err != nil {
		return nil, err
	}
	ctx, done, err := api.enter(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	return api.api.TraceBlockByNumber(ctx, number, config)
}

// enter takes a concurrency slot and attaches the whole-request deadline. The
// returned function releases both and must be called exactly once.
func (api *RestrictedDebugAPI) enter(ctx context.Context) (context.Context, func(), error) {
	release, err := api.limiter.Acquire()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, api.requestTimeout)
	return ctx, func() {
		cancel()
		release()
	}, nil
}

// validateRestrictedTraceConfig rejects any tracing configuration outside what
// the profile permits. Every rejection is explicit rather than a silent clamp,
// so an operator reading the caller's error can tell what was asked for.
func validateRestrictedTraceConfig(config *TraceConfig) error {
	if config == nil {
		return rpc.NewInvalidParamsError(fmt.Sprintf("tracer is required on this transport and must be %q", restrictedTracer))
	}
	if config.Tracer == nil || *config.Tracer != restrictedTracer {
		return rpc.NewInvalidParamsError(fmt.Sprintf("tracer must be %q on this transport", restrictedTracer))
	}
	// The embedded struct logger config is only meaningful for the struct
	// logger, which this profile does not permit.
	if config.Config != nil && *config.Config != (logger.Config{}) {
		return rpc.NewInvalidParamsError("struct logger options are not available on this transport")
	}
	if config.Timeout != nil {
		timeout, err := time.ParseDuration(*config.Timeout)
		if err != nil {
			return rpc.NewInvalidParamsError(fmt.Sprintf("invalid timeout: %v", err))
		}
		if timeout <= 0 || timeout > restrictedMaxTraceTimeout {
			return rpc.NewInvalidParamsError(fmt.Sprintf("timeout %s exceeds the limit of %s on this transport", timeout, restrictedMaxTraceTimeout))
		}
	}
	if config.Reexec != nil && *config.Reexec > restrictedMaxReexec {
		return rpc.NewInvalidParamsError(fmt.Sprintf("reexec %d exceeds the limit of %d on this transport", *config.Reexec, restrictedMaxReexec))
	}
	if len(config.TracerConfig) > 0 {
		// json.Valid rejects concatenated JSON values that Decoder.More cannot
		// detect at the top level (it only reports position inside an array or
		// object). This closes the gap for inputs like `{"onlyTopCall":true}{"x":1}`.
		if !json.Valid(config.TracerConfig) {
			return rpc.NewInvalidParamsError("invalid tracerConfig: invalid JSON")
		}
		dec := json.NewDecoder(bytes.NewReader(config.TracerConfig))
		dec.DisallowUnknownFields()
		var restricted RestrictedCallTracerConfig
		if err := dec.Decode(&restricted); err != nil {
			return rpc.NewInvalidParamsError(fmt.Sprintf("invalid tracerConfig: %v", err))
		}
	}
	return nil
}
