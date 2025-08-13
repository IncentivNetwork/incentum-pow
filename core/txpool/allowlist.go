package txpool

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/log"
)

var (
    ErrSenderNotAllowed   = fmt.Errorf("sender not allowed by allowlist")
    ErrReceiverNotAllowed = fmt.Errorf("receiver not allowed by allowlist")
)

const (
    // Standard path within the repo – can be changed via SetAllowlistFilePath
    defaultAllowlistPath = "allowlist.json"

    // How often we check for file changes
    allowlistCheckInterval = 5 * time.Second
)

// allowlistData is the JSON schema on disk
// {
//   "allowed_senders":   [ "0x...", ... ],
//   "allowed_receivers": [ "0x...", ... ]
// }

type allowlistData struct {
    Senders   []string `json:"allowed_senders"`
    Receivers []string `json:"allowed_receivers"`
}

var (
    allowlistFilePath    = defaultAllowlistPath
    lastAllowlistModTime time.Time

    // Maps for O(1) lookups
    allowedSenders   = make(map[common.Address]struct{})
    allowedReceivers = make(map[common.Address]struct{})

    mu sync.RWMutex // protects above maps + path + ModTime
)

// -----------------------------------------------------------------------------
// Init – loads file and starts background ticker
// -----------------------------------------------------------------------------

func init() {
    loadAllowlistFromFile()

    go func() {
        ticker := time.NewTicker(allowlistCheckInterval)
        defer ticker.Stop()
        for range ticker.C {
            checkAndReloadAllowlist()
        }
    }()
}

// -----------------------------------------------------------------------------
// Public helpers
// -----------------------------------------------------------------------------

// SetAllowlistFilePath allows tests or user config to change the path.
// Reloads immediately.
func SetAllowlistFilePath(path string) {
    mu.Lock()
    allowlistFilePath = path
    mu.Unlock()

    loadAllowlistFromFile()
}

// IsSenderAllowed checks only the sender list; if sender list is empty, it's "allowed".
func IsSenderAllowed(addr common.Address) bool {
    mu.RLock()
    defer mu.RUnlock()

    // If both lists are empty, allow all transactions (AllowList inactive)
    if len(allowedSenders) == 0 && len(allowedReceivers) == 0 {
        return true
    }

    // If only sender list is empty, allow all senders
    if len(allowedSenders) == 0 {
        return true
    }

    // Check if the address is contained in the sender list
    _, allowed := allowedSenders[addr]
    log.Debug("allowlist: sender check result", "address", addr.Hex(), "allowed", allowed)
    return allowed
}

// IsReceiverAllowed checks only the receiver list; nil receiver (contract creation) is allowed.
func IsReceiverAllowed(addr *common.Address) bool {
    mu.RLock()
    defer mu.RUnlock()

    if addr == nil { // contract creation always allowed
        return true
    }

    // If both lists are empty, allow all transactions (AllowList inactive)
    if len(allowedSenders) == 0 && len(allowedReceivers) == 0 {
        return true
    }
    
    // If only receiver list is empty, allow all receivers
    if len(allowedReceivers) == 0 {
        return true
    }
    
    // Check if the address is contained in the receiver list
    _, allowed := allowedReceivers[*addr]
    log.Debug("allowlist: receiver check result", "address", addr.Hex(), "allowed", allowed)
    return allowed
}

// SaveAllowlistToFile writes the current in-memory state to the JSON file.
func SaveAllowlistToFile() error {
    mu.RLock()
    data := allowlistData{
        Senders:   make([]string, 0, len(allowedSenders)),
        Receivers: make([]string, 0, len(allowedReceivers)),
    }
    for a := range allowedSenders {
        data.Senders = append(data.Senders, a.Hex())
    }
    for a := range allowedReceivers {
        data.Receivers = append(data.Receivers, a.Hex())
    }
    mu.RUnlock()

    raw, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return err
    }

    dir := filepath.Dir(allowlistFilePath)
    if dir != "" && dir != "." {
        if err := os.MkdirAll(dir, 0o755); err != nil {
            return err
        }
    }
    return os.WriteFile(allowlistFilePath, raw, 0o644)
}

// -----------------------------------------------------------------------------
// Internal code
// -----------------------------------------------------------------------------

func checkAndReloadAllowlist() {
    mu.RLock()
    path := allowlistFilePath
    mu.RUnlock()

    fi, err := os.Stat(path)
    if err != nil {
        if !os.IsNotExist(err) {
            log.Warn("allowlist: stat error", "err", err)
        }
        return
    }

    mod := fi.ModTime()
    mu.RLock()
    reloadNeeded := mod.After(lastAllowlistModTime)
    mu.RUnlock()

    if reloadNeeded {
        log.Info("allowlist: file changed – reloading", "path", path)
        loadAllowlistFromFile()
    }
}

func loadAllowlistFromFile() {
    mu.RLock()
    path := allowlistFilePath
    mu.RUnlock()

    raw, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            log.Info("allowlist: no file – creating default", "path", path)
            // Create file with empty list
            if err := SaveAllowlistToFile(); err != nil {
                log.Warn("allowlist: cannot create default", "err", err)
            }
        } else {
            log.Warn("allowlist: read error", "err", err)
        }
        return
    }

    var data allowlistData
    if err := json.Unmarshal(raw, &data); err != nil {
        log.Warn("allowlist: invalid JSON, ignoring", "err", err)
        return
    }

    tempSenders := make(map[common.Address]struct{}, len(data.Senders))
    tempReceivers := make(map[common.Address]struct{}, len(data.Receivers))

    for _, s := range data.Senders {
        if common.IsHexAddress(s) {
            tempSenders[common.HexToAddress(s)] = struct{}{}
        }
    }
    for _, r := range data.Receivers {
        if common.IsHexAddress(r) {
            tempReceivers[common.HexToAddress(r)] = struct{}{}
        }
    }

    fi, _ := os.Stat(path) // Error already handled above

    mu.Lock()
    allowedSenders = tempSenders
    allowedReceivers = tempReceivers
    lastAllowlistModTime = fi.ModTime()
    mu.Unlock()

    log.Info("allowlist: loaded", "senders", len(tempSenders), "receivers", len(tempReceivers))
}
