package txpool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestAllowlistBasicFunctionality(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	addr2 := common.HexToAddress("0x0987654321098765432109876543210987654321")

	// Test empty allowlist (should allow all)
	if !IsSenderAllowed(addr1) {
		t.Error("Empty allowlist should allow all senders")
	}
	if !IsReceiverAllowed(&addr1) {
		t.Error("Empty allowlist should allow all receivers")
	}

	// Create allowlist with specific addresses
	data := allowlistData{
		Senders:   []string{addr1.Hex()},
		Receivers: []string{addr2.Hex()},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)

	// Reload and test
	loadAllowlistFromFile()

	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should be allowed as sender")
	}
	if IsSenderAllowed(addr2) {
		t.Error("addr2 should not be allowed as sender")
	}
	if IsReceiverAllowed(&addr1) {
		t.Error("addr1 should not be allowed as receiver")
	}
	if !IsReceiverAllowed(&addr2) {
		t.Error("addr2 should be allowed as receiver")
	}
}

func TestAllowlistContractCreation(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	// Contract creation (nil receiver) should always be allowed
	if !IsReceiverAllowed(nil) {
		t.Error("Contract creation should always be allowed")
	}

	// Even with restricted receiver list
	data := allowlistData{
		Receivers: []string{"0x1234567890123456789012345678901234567890"},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	if !IsReceiverAllowed(nil) {
		t.Error("Contract creation should be allowed even with restricted receiver list")
	}
}

func TestAllowlistFileReload(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Create initial file
	data := allowlistData{Senders: []string{addr1.Hex()}}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should be allowed initially")
	}

	// Wait a bit to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Update file
	data = allowlistData{Senders: []string{}}
	raw, _ = json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)

	// Trigger reload check
	checkAndReloadAllowlist()

	if !IsSenderAllowed(addr1) {
		t.Error("Empty sender list should allow all senders")
	}
}

func TestAllowlistInvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	// Write invalid JSON
	os.WriteFile(testFile, []byte("invalid json"), 0644)

	// Should not panic and should maintain previous state
	loadAllowlistFromFile()

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	if !IsSenderAllowed(addr) {
		t.Error("Invalid JSON should not affect allowlist behavior")
	}
}

func TestAllowlistPartialLists(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	addr2 := common.HexToAddress("0x0987654321098765432109876543210987654321")

	// Test only sender list populated
	data := allowlistData{
		Senders:   []string{addr1.Hex()},
		Receivers: []string{},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should be allowed as sender")
	}
	if IsSenderAllowed(addr2) {
		t.Error("addr2 should not be allowed as sender")
	}
	if !IsReceiverAllowed(&addr1) {
		t.Error("Empty receiver list should allow all receivers")
	}
	if !IsReceiverAllowed(&addr2) {
		t.Error("Empty receiver list should allow all receivers")
	}

	// Test only receiver list populated
	data = allowlistData{
		Senders:   []string{},
		Receivers: []string{addr2.Hex()},
	}
	raw, _ = json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	if !IsSenderAllowed(addr1) {
		t.Error("Empty sender list should allow all senders")
	}
	if !IsSenderAllowed(addr2) {
		t.Error("Empty sender list should allow all senders")
	}
	if IsReceiverAllowed(&addr1) {
		t.Error("addr1 should not be allowed as receiver")
	}
	if !IsReceiverAllowed(&addr2) {
		t.Error("addr2 should be allowed as receiver")
	}
}

func TestAllowlistConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	addr2 := common.HexToAddress("0x0987654321098765432109876543210987654321")

	// Create initial allowlist
	data := allowlistData{
		Senders:   []string{addr1.Hex()},
		Receivers: []string{addr2.Hex()},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	const numGoroutines = 100
	const numOperations = 1000

	// Test concurrent reads
	t.Run("ConcurrentReads", func(t *testing.T) {
		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer func() { done <- true }()
				for j := 0; j < numOperations; j++ {
					IsSenderAllowed(addr1)
					IsSenderAllowed(addr2)
					IsReceiverAllowed(&addr1)
					IsReceiverAllowed(&addr2)
					IsReceiverAllowed(nil)
				}
			}()
		}

		for i := 0; i < numGoroutines; i++ {
			<-done
		}
	})

	// Test concurrent reads with file reloads
	t.Run("ConcurrentReadsWithReloads", func(t *testing.T) {
		done := make(chan bool, numGoroutines+1)

		// Reader goroutines
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer func() { done <- true }()
				for j := 0; j < numOperations/10; j++ {
					IsSenderAllowed(addr1)
					IsReceiverAllowed(&addr2)
				}
			}()
		}

		// Writer goroutine that modifies the file
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 10; j++ {
				time.Sleep(time.Millisecond)
				
				// Alternate between two different configurations
				var newData allowlistData
				if j%2 == 0 {
					newData = allowlistData{
						Senders:   []string{addr1.Hex()},
						Receivers: []string{addr2.Hex()},
					}
				} else {
					newData = allowlistData{
						Senders:   []string{addr2.Hex()},
						Receivers: []string{addr1.Hex()},
					}
				}
				
				raw, _ := json.Marshal(newData)
				os.WriteFile(testFile, raw, 0644)
				loadAllowlistFromFile()
			}
		}()

		for i := 0; i < numGoroutines+1; i++ {
			<-done
		}
	})
}

func TestAllowlistFileRemoval(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr1 := common.HexToAddress("0x1234567890123456789012345678901234567890")
	addr2 := common.HexToAddress("0x0987654321098765432109876543210987654321")

	// Create restrictive allowlist
	data := allowlistData{
		Senders:   []string{addr1.Hex()},
		Receivers: []string{addr2.Hex()},
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(testFile, raw, 0644)
	loadAllowlistFromFile()

	// Verify restrictions are in place
	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should be allowed as sender")
	}
	if IsSenderAllowed(addr2) {
		t.Error("addr2 should not be allowed as sender")
	}
	if IsReceiverAllowed(&addr1) {
		t.Error("addr1 should not be allowed as receiver")
	}
	if !IsReceiverAllowed(&addr2) {
		t.Error("addr2 should be allowed as receiver")
	}

	// Remove the file
	os.Remove(testFile)

	// Trigger reload check - should handle missing file gracefully
	checkAndReloadAllowlist()

	// After file removal, behavior should remain the same as the last valid state
	// (allowlist doesn't reset to "allow all" when file is missing)
	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should still be allowed as sender after file removal")
	}
	if IsSenderAllowed(addr2) {
		t.Error("addr2 should still not be allowed as sender after file removal")
	}

	// Test what happens when we try to load from non-existent file
	loadAllowlistFromFile()

	// The function should handle missing file gracefully and maintain previous state
	if !IsSenderAllowed(addr1) {
		t.Error("addr1 should still be allowed as sender after loadAllowlistFromFile")
	}
}

func TestAllowlistEmptyFileCreation(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_allowlist.json")

	SetAllowlistFilePath(testFile)
	defer SetAllowlistFilePath(defaultAllowlistPath)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Ensure file doesn't exist
	os.Remove(testFile)

	// Try to load from non-existent file - should create default empty file
	loadAllowlistFromFile()

	// Check that file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Default allowlist file should have been created")
	}

	// With empty allowlist, all addresses should be allowed
	if !IsSenderAllowed(addr) {
		t.Error("Empty allowlist should allow all senders")
	}
	if !IsReceiverAllowed(&addr) {
		t.Error("Empty allowlist should allow all receivers")
	}

	// Verify file content is valid JSON with empty lists
	raw, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	var data allowlistData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("Created file should contain valid JSON: %v", err)
	}

	if len(data.Senders) != 0 || len(data.Receivers) != 0 {
		t.Error("Default allowlist should have empty sender and receiver lists")
	}
}
