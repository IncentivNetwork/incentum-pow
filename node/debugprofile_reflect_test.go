// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

// This test lives in the external test package because it reflects over the
// real debug services, which import node.
package node_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/les"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

// debugServices are the service types registered under the debug namespace.
// A service missing here would leave its methods unclassified in the node's
// eyes; adding one is part of introducing it.
var debugServices = []interface{}{
	new(eth.DebugAPI),
	new(ethapi.DebugAPI),
	new(tracers.API),
	new(debug.HandlerT),
	new(les.DebugAPI),
}

// unrestrictedOnlyDebugMethods are the debug methods that must never be
// reachable on a restricted transport. They stay available over IPC.
//
// This list exists so that a new upstream debug_* method fails the test until
// somebody decides which side of the line it belongs on. Do not add a method
// here without reading what it does: several of these rewind the chain, write
// files at caller-chosen paths, or run caller-supplied JavaScript.
var unrestrictedOnlyDebugMethods = []string{
	// eth.DebugAPI
	"accountRange",
	"dumpBlock",
	"getAccessibleState",
	"getBadBlocks",
	"getModifiedAccountsByHash",
	"getModifiedAccountsByNumber",
	"preimage",
	"setTrieFlushInterval",
	"storageRangeAt",

	// internal/ethapi.DebugAPI
	"chaindbCompact",
	"chaindbProperty",
	"dbAncient",
	"dbAncients",
	"dbGet",
	"getRawBlock",
	"getRawHeader",
	"getRawReceipts",
	"getRawTransaction",
	"printBlock",
	"seedHash",
	"setHead",

	// eth/tracers.API
	"intermediateRoots",
	"standardTraceBadBlockToFile",
	"standardTraceBlockToFile",
	"traceBadBlock",
	"traceBlock",
	"traceBlockByHash",
	"traceBlockFromFile",
	"traceCall",
	"traceChain",

	// internal/debug.HandlerT
	"backtraceAt",
	"blockProfile",
	"cpuProfile",
	"freeOSMemory",
	"gcStats",
	"goTrace",
	"memStats",
	"mutexProfile",
	"setBlockProfileRate",
	"setGCPercent",
	"setMutexProfileFraction",
	"stacks",
	"startCPUProfile",
	"startGoTrace",
	"stopCPUProfile",
	"stopGoTrace",
	"verbosity",
	"vmodule",
	"writeBlockProfile",
	"writeMemProfile",
	"writeMutexProfile",

	// les.DebugAPI
	"freezeClient",
}

// TestDebugMethodsAreClassified is the fail-closed regression guarantee: every
// method the debug namespace offers is either in the profile or explicitly
// excluded from restricted transports, and nothing is left undecided.
func TestDebugMethodsAreClassified(t *testing.T) {
	profile := profileMethodSet(t)
	unrestricted := make(map[string]bool, len(unrestrictedOnlyDebugMethods))
	for _, method := range unrestrictedOnlyDebugMethods {
		if profile[method] {
			t.Fatalf("debug_%s is listed as both profile-visible and unrestricted-only", method)
		}
		if unrestricted[method] {
			t.Fatalf("debug_%s is listed twice as unrestricted-only", method)
		}
		unrestricted[method] = true
	}

	var unclassified []string
	seen := make(map[string]bool)
	for _, service := range debugServices {
		for _, method := range exportedMethods(service) {
			seen[method] = true
			if !profile[method] && !unrestricted[method] {
				unclassified = append(unclassified, method)
			}
		}
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Fatalf("unclassified debug methods: %s\n"+
			"Add each to the %s profile in rpc/debugprofile.go, or to unrestrictedOnlyDebugMethods in this file.",
			strings.Join(unclassified, ", "), rpc.DebugProfileTraceIndexerV1)
	}

	// The classification must not describe methods that no longer exist, or it
	// stops being evidence of anything.
	for method := range profile {
		if !seen[method] {
			t.Errorf("profile method debug_%s is not offered by any debug service", method)
		}
	}
	for method := range unrestricted {
		if !seen[method] {
			t.Errorf("unrestricted-only method debug_%s is not offered by any debug service", method)
		}
	}
}

// TestTraceIndexerProfileContents pins the profile's contents. Widening it is a
// deliberate act that has to change this test too.
func TestTraceIndexerProfileContents(t *testing.T) {
	methods, err := rpc.DebugProfileMethods(rpc.DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"traceBlockByNumber", "traceTransaction"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("%s = %v, want exactly %v", rpc.DebugProfileTraceIndexerV1, methods, want)
	}
}

// TestIPCRegistersFullDebugNamespace checks that the unrestricted registration
// path — the one IPC uses — still exposes every debug method.
func TestIPCRegistersFullDebugNamespace(t *testing.T) {
	server := rpc.NewServer()
	var apis []rpc.API
	for _, service := range debugServices {
		apis = append(apis, rpc.API{Namespace: rpc.DebugNamespace, Service: service})
	}
	if err := node.RegisterApis(apis, nil, server); err != nil {
		t.Fatal(err)
	}
	var modules map[string]string
	client := rpc.DialInProc(server)
	defer client.Close()
	if err := client.Call(&modules, "rpc_modules"); err != nil {
		t.Fatal(err)
	}
	if _, ok := modules[rpc.DebugNamespace]; !ok {
		t.Fatalf("debug namespace missing over IPC: %v", modules)
	}

	// Every classified method must resolve to a callback: a -32601 here would
	// mean the unrestricted surface shrank.
	subscriptions := subscriptionMethods()
	for _, method := range append(profileMethodList(t), unrestrictedOnlyDebugMethods...) {
		if subscriptions[method] {
			// Subscription callbacks are reached through debug_subscribe, not
			// by name, so calling them directly proves nothing.
			continue
		}
		var result interface{}
		err := client.Call(&result, rpc.DebugNamespace+"_"+method)
		if err == nil {
			continue
		}
		rpcErr, ok := err.(rpc.Error)
		if ok && rpcErr.ErrorCode() == -32601 {
			t.Errorf("debug_%s is not registered on an unrestricted transport", method)
		}
		// Any other error means the callback exists and rejected the empty
		// argument list, which is all this test needs to know.
	}
}

// subscriptionMethods returns the debug methods the service registry treats as
// subscriptions: those taking a context and returning (*rpc.Subscription, error).
func subscriptionMethods() map[string]bool {
	subscriptionType := reflect.TypeOf((*rpc.Subscription)(nil))
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()

	subscriptions := make(map[string]bool)
	for _, service := range debugServices {
		typ := reflect.TypeOf(service)
		for i := 0; i < typ.NumMethod(); i++ {
			method := typ.Method(i)
			fn := method.Type
			if fn.NumIn() < 2 || fn.In(1) != contextType {
				continue
			}
			if fn.NumOut() == 2 && fn.Out(0) == subscriptionType {
				name := method.Name
				subscriptions[strings.ToLower(name[:1])+name[1:]] = true
			}
		}
	}
	return subscriptions
}

func profileMethodList(t *testing.T) []string {
	t.Helper()

	methods, err := rpc.DebugProfileMethods(rpc.DebugProfileTraceIndexerV1)
	if err != nil {
		t.Fatal(err)
	}
	return methods
}

func profileMethodSet(t *testing.T) map[string]bool {
	t.Helper()

	set := make(map[string]bool)
	for _, method := range profileMethodList(t) {
		set[method] = true
	}
	return set
}

// exportedMethods returns the RPC wire names of a service's exported methods,
// applying the same first-letter lowercasing the service registry does.
func exportedMethods(service interface{}) []string {
	typ := reflect.TypeOf(service)
	methods := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		methods = append(methods, strings.ToLower(name[:1])+name[1:])
	}
	return methods
}
