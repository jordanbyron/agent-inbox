package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MaxLogBytes is how large watcher.log grows before it is moved to
// watcher.log.1, replacing the one before, so the two never pass twice this.
const MaxLogBytes = 1 << 20

// LogPath is the watcher's log in a state directory.
func LogPath(dir string) string {
	return filepath.Join(dir, "watcher.log")
}

// LogFile is watcher.log, opened for appending. It is safe for concurrent use.
type LogFile struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

// OpenLog opens the watcher's log in dir.
func OpenLog(dir string) (*LogFile, error) {
	l := &LogFile{path: LogPath(dir)}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *LogFile) open() error {
	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening the watcher log: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("opening the watcher log: %w", err)
	}
	l.file, l.size = file, info.Size()
	return nil
}

// Write appends p, first moving the log aside if p would take it past
// MaxLogBytes.
func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > MaxLogBytes {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *LogFile) rotate() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("rotating the watcher log: %w", err)
	}
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return fmt.Errorf("rotating the watcher log: %w", err)
	}
	return l.open()
}

// Close closes the log.
func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
