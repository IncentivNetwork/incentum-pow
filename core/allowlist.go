package core

import (
    "fmt"
    "sync/atomic"

    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/params"
)

// ErrReceiverNotAllowed is returned when a transaction is sent to an address 
// that is not allowed by the allowlist.
var (
    ErrReceiverNotAllowed = fmt.Errorf("receiver not allowed by allowlist")
)

// precomputedAllowlistEntry is an internal, efficient representation of a single
// allowlist window for an address.
//
// If activateBlock is nil, the entry is active from block 0.
// If deactivateBlock is nil, the entry is active indefinitely after activation.
type precomputedAllowlistEntry struct {
    address        common.Address
    activateBlock  *uint64
    deactivateBlock *uint64
}

// allowlistState holds the active allowlist entries. It is stored in an atomic.Value
// to allow lock-free reads from IsReceiverAllowed.
var allowlistState atomic.Value // stores []precomputedAllowlistEntry

// SetAllowlist replaces the current allowlist with the content provided from the
// chain configuration. Callers should ensure this is invoked during startup after
// the chain configuration is available.
func SetAllowlist(entries []params.AllowlistEntry) {
    pre := make([]precomputedAllowlistEntry, 0, len(entries))
    for _, e := range entries {
        var (
            a *uint64
            d *uint64
        )
        if e.ActivateBlock != nil {
            v := e.ActivateBlock.Uint64()
            a = &v
        }
        if e.DeactivateBlock != nil {
            v := e.DeactivateBlock.Uint64()
            d = &v
        }
        pre = append(pre, precomputedAllowlistEntry{
            address:         e.Address,
            activateBlock:   a,
            deactivateBlock: d,
        })
    }
    allowlistState.Store(pre)
}

// IsReceiverAllowed reports whether the given address is allowed at the provided
// block number, based on the currently configured allowlist windows.
//
// Semantics:
// - If no allowlist is configured (empty or unset), all addresses are allowed.
// - If an allowlist is configured but no entries are active at the current block,
//   all addresses are allowed (allowlist inactive for this block).
// - Otherwise, an address is allowed if ANY entry for that address is active for the block.
// - An entry is active if block >= activateBlock (or activateBlock is nil) AND
//   (deactivateBlock is nil OR block < deactivateBlock).
// - If to is nil, it means the transaction is a contract creation, which is always allowed.
func IsReceiverAllowed(to *common.Address, block uint64) bool {
    v := allowlistState.Load()
    if v == nil {
        // Not initialized -> treat as no restrictions
        return true
    }
    entries, _ := v.([]precomputedAllowlistEntry)
    if len(entries) == 0 {
        return true
    }

	// Contract creation always allowed
    if to == nil {
        return true
    }

    addr := *to
    anyActive := false
    for _, e := range entries {
        // Evaluate whether this entry is active at the given block
        active := true
        if e.activateBlock != nil && block < *e.activateBlock {
            active = false
        }
        if e.deactivateBlock != nil && block >= *e.deactivateBlock {
            active = false
        }
        if active {
            anyActive = true
            if e.address == addr {
                return true
            }
        }
    }
    // If no entries are active at this block, allow all
    if !anyActive {
        return true
    }
    return false
}
