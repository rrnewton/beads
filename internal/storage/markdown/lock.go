package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	lockTimeout   = 30 * time.Second
	lockRetryWait = 100 * time.Millisecond
)

// lockFile acquires a lock on an issue file
// Returns the lock or an error if unable to acquire within timeout
func (m *MarkdownStorage) lockFile(issueID string) (*lock, error) {
	m.locksMu.Lock()
	if existingLock, exists := m.locks[issueID]; exists {
		m.locksMu.Unlock()
		return existingLock, nil // Already locked by us
	}
	m.locksMu.Unlock()

	issuePath := m.getIssuePath(issueID)
	lockPath := fmt.Sprintf("%s.lock.%d", issuePath, m.pid)

	deadline := time.Now().Add(lockTimeout)
	for time.Now().Before(deadline) {
		// Try to acquire lock by renaming the file
		err := os.Rename(issuePath, lockPath)
		if err == nil {
			// Successfully acquired lock
			lock := &lock{
				issueID:  issueID,
				lockPath: lockPath,
			}

			m.locksMu.Lock()
			m.locks[issueID] = lock
			m.locksMu.Unlock()

			return lock, nil
		}

		// Check if lock is held by another process
		if os.IsNotExist(err) {
			// Issue doesn't exist - this might be intentional (creating new issue)
			// or the file is locked by someone else
			// Check for lock files
			lockFiles, _ := filepath.Glob(issuePath + ".lock.*")
			if len(lockFiles) > 0 {
				// Someone else holds the lock
				// Check if it's stale
				if m.tryBreakStaleLock(lockFiles) {
					continue // Retry after breaking stale lock
				}

				// Check lock priority
				holderPID := m.extractPID(lockFiles[0])
				if holderPID > 0 && holderPID < m.pid {
					// Higher priority process holds lock - back off
					time.Sleep(lockRetryWait)
					continue
				}
			}
		}

		time.Sleep(lockRetryWait)
	}

	return nil, fmt.Errorf("timeout acquiring lock for %s", issueID)
}

// unlockFile releases a lock on an issue file
func (m *MarkdownStorage) unlockFile(lock *lock) error {
	issuePath := m.getIssuePath(lock.issueID)

	// Rename lock file back to original
	if err := os.Rename(lock.lockPath, issuePath); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	m.locksMu.Lock()
	delete(m.locks, lock.issueID)
	m.locksMu.Unlock()

	return nil
}

// commitFile atomically commits changes from temp file to the actual file
// This is a two-step process:
// 1. Rename temp file to the actual file (commits changes)
// 2. Rename lock file to trash (releases lock)
func (m *MarkdownStorage) commitFile(lock *lock, tempPath string) error {
	issuePath := m.getIssuePath(lock.issueID)
	trashPath := fmt.Sprintf("%s.trash.%d", issuePath, m.pid)

	// Step 1: Commit changes (temp -> actual)
	if err := os.Rename(tempPath, issuePath); err != nil {
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	// Step 2: Release lock (lock -> trash)
	if err := os.Rename(lock.lockPath, trashPath); err != nil {
		// Lock file may have been moved/deleted - non-fatal
		// The important part (committing changes) succeeded
	}

	// Step 3: Cleanup trash
	_ = os.Remove(trashPath)

	m.locksMu.Lock()
	delete(m.locks, lock.issueID)
	m.locksMu.Unlock()

	return nil
}

// cleanupStaleLocks removes lock files from dead processes
func (m *MarkdownStorage) cleanupStaleLocks() error {
	// Find all lock, tmp, and trash files
	patterns := []string{
		filepath.Join(m.issuesDir, "*.lock.*"),
		filepath.Join(m.issuesDir, "*.tmp.*"),
		filepath.Join(m.issuesDir, "*.trash.*"),
	}

	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, file := range files {
			pid := m.extractPID(file)
			if pid == 0 {
				continue
			}

			// Check if process exists
			if !m.processExists(pid) {
				// Process is dead, remove stale file
				_ = os.Remove(file)

				// For lock files, try to restore the original file
				if strings.Contains(file, ".lock.") {
					issuePath := m.getLockIssuePath(file)
					if issuePath != "" {
						// Lock file exists but process is dead
						// Rename it back to the original file
						_ = os.Rename(file, issuePath)
					}
				}
			}
		}
	}

	return nil
}

// tryBreakStaleLock attempts to break stale locks
// Returns true if a stale lock was broken
func (m *MarkdownStorage) tryBreakStaleLock(lockFiles []string) bool {
	for _, lockFile := range lockFiles {
		pid := m.extractPID(lockFile)
		if pid == 0 {
			continue
		}

		if !m.processExists(pid) {
			// Stale lock - remove it and restore original file
			issuePath := m.getLockIssuePath(lockFile)
			if issuePath != "" {
				_ = os.Rename(lockFile, issuePath)
				return true
			}
		}
	}
	return false
}

// extractPID extracts the PID from a lock/tmp/trash filename
// Example: "prefix-123.md.lock.12345" -> 12345
func (m *MarkdownStorage) extractPID(filename string) int {
	parts := strings.Split(filepath.Base(filename), ".")
	if len(parts) < 2 {
		return 0
	}

	pidStr := parts[len(parts)-1]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0
	}

	return pid
}

// getLockIssuePath returns the original issue path from a lock file path
// Example: ".../prefix-123.md.lock.12345" -> ".../prefix-123.md"
func (m *MarkdownStorage) getLockIssuePath(lockPath string) string {
	// Remove .lock.<pid> suffix
	base := filepath.Base(lockPath)
	parts := strings.Split(base, ".lock.")
	if len(parts) != 2 {
		return ""
	}

	dir := filepath.Dir(lockPath)
	return filepath.Join(dir, parts[0])
}

// processExists checks if a process with the given PID exists
func (m *MarkdownStorage) processExists(pid int) bool {
	// Try to find the process
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds, so we need to signal it
	// Signal 0 doesn't actually send a signal but checks if we can
	err = process.Signal(os.Signal(nil))
	if err != nil {
		return false
	}

	return true
}

// getIssuePath returns the file path for an issue
func (m *MarkdownStorage) getIssuePath(issueID string) string {
	return filepath.Join(m.issuesDir, issueID+".md")
}

// getTempPath returns a temporary file path for this process
func (m *MarkdownStorage) getTempPath(issueID string) string {
	return fmt.Sprintf("%s.tmp.%d", m.getIssuePath(issueID), m.pid)
}
