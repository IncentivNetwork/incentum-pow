// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package node

import (
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

// restrictedDebugRequestTimeout bounds a single restricted debug request. A
// block trace fans out over every transaction in the block, so the per-
// transaction timeout does not bound the request on its own.
const restrictedDebugRequestTimeout = 30 * time.Second

// debugTransport describes the debug-namespace capability configuration of one
// untrusted transport.
type debugTransport struct {
	name        string // flag prefix, "http" or "ws"
	modules     []string
	profile     string
	allowUnsafe bool
}

// debugEnabled reports whether the module list names the debug namespace.
func (t debugTransport) debugEnabled() bool {
	for _, module := range t.modules {
		if module == rpc.DebugNamespace {
			return true
		}
	}
	return false
}

// registersEverything reports whether the transport has no module filter at
// all, in which case every namespace is registered, debug included. This is a
// separate case from debugEnabled: the operator never named debug, so it is
// warned about rather than made a startup error.
func (t debugTransport) registersEverything() bool {
	return len(t.modules) == 0
}

// validate checks the transport's debug configuration. Every ambiguous
// combination is an error: the debug namespace is only ever served on an
// untrusted transport when the operator has said, explicitly, which of the two
// behaviours they want.
func (t debugTransport) validate() error {
	switch {
	case t.profile != "" && t.allowUnsafe:
		return fmt.Errorf("--%s.debug-profile and --%s.allow-unsafe-debug are mutually exclusive", t.name, t.name)

	case t.profile != "" && !t.debugEnabled() && !t.registersEverything():
		return fmt.Errorf("--%s.debug-profile is set but %q is not in --%s.api", t.name, rpc.DebugNamespace, t.name)

	case t.profile != "":
		_, err := rpc.DebugProfileMethods(t.profile)
		return err

	case t.registersEverything() && !t.allowUnsafe:
		return fmt.Errorf("empty --%s.api exposes every namespace, including %q: "+
			"list the namespaces explicitly, set --%s.debug-profile=%s to restrict debug, "+
			"or set --%s.allow-unsafe-debug to opt into the legacy full surface",
			t.name, rpc.DebugNamespace, t.name, rpc.DebugProfileTraceIndexerV1, t.name)

	case t.debugEnabled() && !t.allowUnsafe:
		return fmt.Errorf("the %q namespace is exposed on --%s.api without --%s.debug-profile: "+
			"it grants any caller reaching this transport destructive and resource-exhausting methods. "+
			"Set --%s.debug-profile=%s to expose only the indexer trace methods, "+
			"or --%s.allow-unsafe-debug to keep the full namespace",
			rpc.DebugNamespace, t.name, t.name, t.name, rpc.DebugProfileTraceIndexerV1, t.name)
	}
	return nil
}

// options returns the restricted debug configuration to apply to the
// transport, or nil when the transport is not running a profile. The limiter is
// shared between transports on purpose: it protects node-wide resources.
func (t debugTransport) options(limiter *rpc.TraceLimiter) *rpc.RestrictedDebugOptions {
	if t.profile == "" {
		return nil
	}
	return &rpc.RestrictedDebugOptions{
		Profile:        t.profile,
		RequestTimeout: restrictedDebugRequestTimeout,
		Limiter:        limiter,
	}
}

// warnUnsafe emits the one-off startup warning for a transport serving the full
// debug namespace, whether the operator asked for it explicitly or inherited it
// from an unfiltered module list. It is called once at startup: a per-request
// warning would only bury it.
func (t debugTransport) warnUnsafe() {
	switch {
	case t.registersEverything():
		log.Warn("Empty API module list exposes every namespace",
			"transport", t.name,
			"flag", "--"+t.name+".api",
			"advice", "list every namespace this interface should serve explicitly")

	case t.profile != "":
		return

	case t.allowUnsafe && t.debugEnabled():
		log.Warn("Full debug namespace exposed on untrusted transport",
			"transport", t.name,
			"flag", "--"+t.name+".allow-unsafe-debug",
			"advice", "restrict with --"+t.name+".debug-profile="+rpc.DebugProfileTraceIndexerV1+" unless this interface is operator-only")
	}
}

// checkDebugProfiles validates the debug configuration of both untrusted
// transports, but only for transports that are actually enabled.
func (c *Config) checkDebugProfiles() error {
	for _, transport := range c.debugTransports() {
		if err := transport.validate(); err != nil {
			return err
		}
	}
	return nil
}

// httpDebugTransport describes the debug configuration of the HTTP interface.
func (c *Config) httpDebugTransport() debugTransport {
	return debugTransport{
		name:        "http",
		modules:     c.HTTPModules,
		profile:     strings.TrimSpace(c.HTTPDebugProfile),
		allowUnsafe: c.HTTPAllowUnsafeDebug,
	}
}

// wsDebugTransport describes the debug configuration of the WebSocket
// interface. WS is covered as thoroughly as HTTP: restricting only HTTP would
// silently reopen the whole surface on the other port.
func (c *Config) wsDebugTransport() debugTransport {
	return debugTransport{
		name:        "ws",
		modules:     c.WSModules,
		profile:     strings.TrimSpace(c.WSDebugProfile),
		allowUnsafe: c.WSAllowUnsafeDebug,
	}
}

// debugTransports returns the enabled untrusted transports. IPC is absent by
// design: it is an operator-only, filesystem-permissioned socket and keeps the
// full debug namespace.
func (c *Config) debugTransports() []debugTransport {
	var transports []debugTransport
	if c.HTTPHost != "" {
		transports = append(transports, c.httpDebugTransport())
	}
	if c.WSHost != "" {
		transports = append(transports, c.wsDebugTransport())
	}
	return transports
}

// debugTraceLimiter returns the limiter shared by all restricted transports, or
// nil when no profile is configured. It ignores whether a transport is
// currently enabled, so that a transport started later through admin_startHTTP
// shares the same limiter rather than getting a second one.
func (c *Config) debugTraceLimiter() *rpc.TraceLimiter {
	if c.httpDebugTransport().profile == "" && c.wsDebugTransport().profile == "" {
		return nil
	}
	max := c.DebugTraceMaxConcurrency
	if max <= 0 {
		max = DefaultDebugTraceMaxConcurrency
	}
	return rpc.NewTraceLimiter(max)
}
