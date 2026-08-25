package main

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const maxLogEntries = 500

type LogEntry struct {
	Ts        string  `json:"ts"`
	Level     string  `json:"level"`
	Logger    string  `json:"logger"`
	Message   string  `json:"message"`
	Exception *string `json:"exception"`
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
}

func NewLogBuffer() *LogBuffer {
	return &LogBuffer{entries: make([]LogEntry, 0, maxLogEntries)}
}

func (b *LogBuffer) Add(level, logger, message string, exception *string) {
	if len(message) > 4000 {
		message = message[:4000] + "…"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) >= maxLogEntries {
		copy(b.entries, b.entries[1:])
		b.entries = b.entries[:len(b.entries)-1]
	}
	b.entries = append(b.entries, LogEntry{
		Ts:        time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level,
		Logger:    logger,
		Message:   message,
		Exception: exception,
	})
}

func (b *LogBuffer) Tail(lines int, level string) []LogEntry {
	if lines < 1 {
		lines = 1
	}
	if lines > 500 {
		lines = 500
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	filtered := b.entries
	if level != "" {
		filtered = make([]LogEntry, 0, len(b.entries))
		for _, e := range b.entries {
			if strings.EqualFold(e.Level, level) {
				filtered = append(filtered, e)
			}
		}
	}
	if len(filtered) <= lines {
		out := make([]LogEntry, len(filtered))
		copy(out, filtered)
		return out
	}
	from := len(filtered) - lines
	out := make([]LogEntry, lines)
	copy(out, filtered[from:])
	return out
}

func teeStdLog(buf *LogBuffer, logger string) {
	log.SetOutput(io.MultiWriter(os.Stderr, &logTee{buf: buf, logger: logger}))
}

type logTee struct {
	buf    *LogBuffer
	logger string
}

func (t *logTee) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	if msg == "" {
		return len(p), nil
	}
	level := "INFO"
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, " fatal") || strings.Contains(lower, "panic"):
		level = "ERROR"
	case strings.Contains(lower, "error") || strings.Contains(lower, " failed"):
		level = "ERROR"
	case strings.Contains(lower, "warn"):
		level = "WARN"
	}
	t.buf.Add(level, t.logger, msg, nil)
	return len(p), nil
}
