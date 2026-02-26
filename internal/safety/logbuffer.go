package safety

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	max     int
	path    string
}

func NewLogBuffer(max int) *LogBuffer {
	return &LogBuffer{max: max}
}

func NewPersistentLogBuffer(max int, path string) *LogBuffer {
	l := &LogBuffer{max: max, path: path}
	l.load()
	return l
}

func (l *LogBuffer) Log(level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
	})
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
	l.save()
}

func (l *LogBuffer) Info(format string, args ...interface{})  { l.Log("info", format, args...) }
func (l *LogBuffer) Warn(format string, args ...interface{})  { l.Log("warn", format, args...) }
func (l *LogBuffer) Error(format string, args ...interface{}) { l.Log("error", format, args...) }

func (l *LogBuffer) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

func (l *LogBuffer) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
	l.save()
}

func (l *LogBuffer) save() {
	if l.path == "" {
		return
	}
	data, _ := json.Marshal(l.entries)
	os.WriteFile(l.path, data, 0644)
}

func (l *LogBuffer) load() {
	if l.path == "" {
		return
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &l.entries)
}
