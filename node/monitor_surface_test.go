// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package node

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

// The whole point of the monitor namespace is that an operator can expose it
// on an untrusted transport instead of admin. That promise is not a property of
// the two methods it ships with — it is a property of the namespace staying
// read-only. Nothing in the existing tests fails if a method is added to
// monitorAPI later, so a future addition would inherit the "safe to expose"
// reputation without anyone re-deciding it. These tests make that a deliberate
// choice: adding a method breaks them until the new name is listed here.

// monitorReadOnlyMethods is the agreed surface of the namespace. A method added
// to monitorAPI must be added here too, which is the point at which someone has
// to ask whether it is still read-only.
var monitorReadOnlyMethods = []string{"nodeInfo", "peerCount"}

// wireNames converts exported Go method names to their JSON-RPC spelling.
func wireNames(typ reflect.Type) []string {
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		names = append(names, strings.ToLower(name[:1])+name[1:])
	}
	sort.Strings(names)
	return names
}

func TestMonitorNamespaceSurfaceIsFixed(t *testing.T) {
	got := wireNames(reflect.TypeOf(&monitorAPI{}))

	want := append([]string(nil), monitorReadOnlyMethods...)
	sort.Strings(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("monitor namespace exposes %v, want exactly %v.\n"+
			"A method added to monitorAPI is served to anyone allowed the monitor "+
			"namespace. If the new method is read-only, add it to "+
			"monitorReadOnlyMethods; if it is not, it does not belong here.", got, want)
	}
}

// TestMonitorNamespaceIsRegistered pins the namespace name. The methods are
// reachable only as monitor_*, and an operator's allow-list names the namespace,
// so a rename would silently drop monitoring off a locked-down transport.
func TestMonitorNamespaceIsRegistered(t *testing.T) {
	stack, err := New(&Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer stack.Close()

	var found *rpc.API
	for i, api := range stack.apis() {
		if api.Namespace == "monitor" {
			found = &stack.apis()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no monitor namespace in the built-in APIs")
	}
	if _, ok := found.Service.(*monitorAPI); !ok {
		t.Fatalf("monitor namespace is served by %T, want *monitorAPI", found.Service)
	}
}

// TestMonitorNamespaceHasNoSubscriptions keeps the namespace usable on HTTP.
// A subscription method would be silently unavailable there — HTTP cannot carry
// notifications — so monitoring that worked over WS would fail over HTTP for a
// reason no error message explains.
func TestMonitorNamespaceHasNoSubscriptions(t *testing.T) {
	srv := rpc.NewServer()
	if err := srv.RegisterName("monitor", &monitorAPI{}); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	typ := reflect.TypeOf(&monitorAPI{})
	subscription := reflect.TypeOf((*rpc.Subscription)(nil))
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		for j := 0; j < method.Type.NumOut(); j++ {
			if method.Type.Out(j) == subscription {
				t.Errorf("%s is a subscription; the monitor namespace must work over HTTP", method.Name)
			}
		}
	}
}
