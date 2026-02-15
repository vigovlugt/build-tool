package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"sync"
)

type Logger struct {
	out          io.Writer
	err          io.Writer
	colorEnabled bool
	prefixWidth  int

	mu sync.Mutex
}

type LoggerOptions struct {
	ColorEnabled bool
	PrefixWidth  int
}

func NewLogger(out io.Writer, err io.Writer, opts LoggerOptions) *Logger {
	return &Logger{
		out:          out,
		err:          err,
		colorEnabled: opts.ColorEnabled,
		prefixWidth:  opts.PrefixWidth,
	}
}

func DetectColorEnabled() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func (l *Logger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.err, format, args...)
}

func (l *Logger) Taskf(taskID TaskID, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.TaskLine(taskID, msg)
}

func (l *Logger) TaskLine(taskID TaskID, line string) {
	prefix := l.taskPrefix(taskID)

	l.mu.Lock()
	defer l.mu.Unlock()
	if line == "" {
		fmt.Fprintf(l.out, "%s\n", prefix)
		return
	}
	fmt.Fprintf(l.out, "%s %s\n", prefix, line)
}

func (l *Logger) taskPrefix(taskID TaskID) string {
	name := string(taskID)
	if l.prefixWidth > 0 {
		name = fmt.Sprintf("%-*s", l.prefixWidth, name)
	}

	if !l.colorEnabled {
		return fmt.Sprintf("%s |", name)
	}

	color := ansiColorForTask(taskID)
	return fmt.Sprintf("%s%s |%s", color, name, ansiReset)
}

const ansiReset = "\x1b[0m"

func ansiColorForTask(taskID TaskID) string {
	// A small set of high-contrast colors that work on light/dark terminals.
	palette := []string{
		"\x1b[36m", // cyan
		"\x1b[32m", // green
		"\x1b[33m", // yellow
		"\x1b[34m", // blue
		"\x1b[35m", // magenta
		"\x1b[91m", // bright red
		"\x1b[92m", // bright green
		"\x1b[94m", // bright blue
		"\x1b[96m", // bright cyan
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(taskID))
	idx := int(h.Sum32()) % len(palette)
	return palette[idx]
}
