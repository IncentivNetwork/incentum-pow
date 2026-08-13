// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package rpc

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DebugNamespace is the RPC namespace the debug methods live under.
const DebugNamespace = "debug"

// DebugProfileTraceIndexerV1 is the capability profile required by the block
// explorer indexer. It allows the two tracing methods Blockscout calls and
// nothing else.
//
// The profile name carries a version suffix on purpose: an indexer that later
// needs an additional method gets a new profile rather than a silent widening
// of this one.
const DebugProfileTraceIndexerV1 = "trace-indexer-v1"

// debugProfiles maps a profile name to the wire method names it allows. The
// names are given without the "debug_" namespace prefix, exactly as the RPC
// service registry stores them.
//
// This table is the single place where a restricted transport's debug surface
// is defined. Registration is fail-closed: a method absent here is never
// registered, and therefore answers with the standard -32601.
var debugProfiles = map[string][]string{
	DebugProfileTraceIndexerV1: {
		"traceTransaction",
		"traceBlockByNumber",
	},
}

// MaxTraceCallsPerBatch is the number of profile-allowed trace calls a single
// JSON-RPC batch may contain on a restricted transport. It is deliberately not
// operator-configurable: it bounds the worst-case cost of one request, which
// the generic batch-element limit alone does not.
const MaxTraceCallsPerBatch = 10

// DebugProfileNames returns the sorted list of known debug capability profiles.
func DebugProfileNames() []string {
	names := make([]string, 0, len(debugProfiles))
	for name := range debugProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DebugProfileMethods returns the sorted wire method names (without namespace
// prefix) allowed by the given profile. It fails for an unknown profile so that
// a typo in the flag can never degrade into a wider surface.
func DebugProfileMethods(profile string) ([]string, error) {
	methods, ok := debugProfiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown debug profile %q (known profiles: %s)", profile, strings.Join(DebugProfileNames(), ", "))
	}
	out := make([]string, len(methods))
	copy(out, methods)
	sort.Strings(out)
	return out, nil
}

// isProfiledTraceMethod reports whether the given fully qualified wire method
// (e.g. "debug_traceBlockByNumber") is allowed by any debug profile. These are
// the calls the per-batch trace cap applies to.
func isProfiledTraceMethod(method string) bool {
	prefix := DebugNamespace + serviceMethodSeparator
	if !strings.HasPrefix(method, prefix) {
		return false
	}
	name := strings.TrimPrefix(method, prefix)
	for _, methods := range debugProfiles {
		for _, allowed := range methods {
			if allowed == name {
				return true
			}
		}
	}
	return false
}

// RestrictedDebugOptions describes the restricted debug service a transport
// wants. It is passed to a RestrictedDebugFactory at registration time.
type RestrictedDebugOptions struct {
	// Profile is the capability profile name, e.g. DebugProfileTraceIndexerV1.
	Profile string

	// RequestTimeout bounds the wall-clock time of a single restricted call.
	// A single trace request can fan out over every transaction in a block, so
	// the per-transaction timeout does not bound it on its own.
	RequestTimeout time.Duration

	// Limiter caps how many restricted calls may run concurrently. It may be
	// shared between transports and may be nil, meaning no limit.
	Limiter *TraceLimiter
}

// RestrictedDebugFactory turns a service registered under the debug namespace
// into its capability-restricted view. It returns a nil service, and no error,
// for a service it does not recognise.
type RestrictedDebugFactory func(service interface{}, opts RestrictedDebugOptions) (interface{}, error)

var (
	restrictedDebugMu        sync.Mutex
	restrictedDebugFactories []RestrictedDebugFactory
)

// RegisterRestrictedDebugFactory adds f to the factories consulted when a
// transport enables a debug profile. Call it from a package init function.
//
// This is a registry rather than an interface implemented by the service
// itself, because every exported method of a service object is registered as an
// RPC method: an interface would add itself to the debug namespace.
func RegisterRestrictedDebugFactory(f RestrictedDebugFactory) {
	restrictedDebugMu.Lock()
	defer restrictedDebugMu.Unlock()

	restrictedDebugFactories = append(restrictedDebugFactories, f)
}

// RestrictedDebugService returns the capability-restricted view of the given
// debug service. It returns a nil service when no registered factory handles
// it; the caller must then leave the service unregistered rather than fall back
// to the unrestricted one.
func RestrictedDebugService(service interface{}, opts RestrictedDebugOptions) (interface{}, error) {
	restrictedDebugMu.Lock()
	factories := make([]RestrictedDebugFactory, len(restrictedDebugFactories))
	copy(factories, restrictedDebugFactories)
	restrictedDebugMu.Unlock()

	for _, factory := range factories {
		restricted, err := factory(service, opts)
		if err != nil {
			return nil, err
		}
		if restricted != nil {
			return restricted, nil
		}
	}
	return nil, nil
}

// TraceLimiter is a non-blocking semaphore bounding the number of concurrent
// tracing calls. Saturation is reported to the caller immediately rather than
// queued: queueing would only convert resource exhaustion into unbounded
// latency. Note this protects the process against accidental overload, it is
// not a defence against a determined adversary — that belongs at the load
// balancer.
type TraceLimiter struct {
	slots chan struct{}
}

// NewTraceLimiter returns a limiter allowing max concurrent calls. A max of
// zero or less means unlimited, in which case nil is returned.
func NewTraceLimiter(max int) *TraceLimiter {
	if max <= 0 {
		return nil
	}
	return &TraceLimiter{slots: make(chan struct{}, max)}
}

// Acquire takes a slot without blocking. On success it returns a release
// function which must be called exactly once. On saturation it returns
// ErrTooManyTraces.
func (l *TraceLimiter) Acquire() (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, nil
	default:
		return nil, ErrTooManyTraces
	}
}

// ErrTooManyTraces is returned when the concurrent trace limit is reached.
var ErrTooManyTraces error = new(tooManyTracesError)

type tooManyTracesError struct{}

func (e *tooManyTracesError) ErrorCode() int { return -32005 }

func (e *tooManyTracesError) Error() string { return "too many concurrent traces" }

// NewInvalidParamsError returns a JSON-RPC error with the standard invalid
// params code (-32602). It lets services outside this package reject arguments
// with the correct code instead of a generic server error.
func NewInvalidParamsError(message string) Error {
	return &invalidParamsError{message}
}

// RegisterNameWithMethods is like RegisterName but registers only the named
// callbacks. Methods outside allowed are never added to the service registry,
// so they are indistinguishable from methods that do not exist and are answered
// with -32601 by the dispatcher.
//
// Registering an unknown method name is an error: the caller's allowlist and
// the receiver must agree, otherwise a renamed method would silently vanish.
func (s *Server) RegisterNameWithMethods(name string, receiver interface{}, allowed []string) error {
	return s.services.registerNameFiltered(name, receiver, allowed)
}
