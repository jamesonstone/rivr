package subprocess

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"sync"
)

const DefaultLimit = 4 << 20

var ErrOutputLimit = errors.New("subprocess output limit exceeded")

type Result struct {
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
}

type limitedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
	stop      func()
	once      sync.Once
}

func Run(command *exec.Cmd) (Result, error) {
	stop := func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
	stdout := &limitedBuffer{limit: DefaultLimit, stop: stop}
	stderr := &limitedBuffer{limit: DefaultLimit, stop: stop}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	result := Result{
		Stdout: stdout.bytes(), Stderr: stderr.bytes(),
		StdoutTruncated: stdout.wasTruncated(), StderrTruncated: stderr.wasTruncated(),
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, outputLimitError(result, err)
	}
	return result, err
}

func Combined(command *exec.Cmd) ([]byte, error) {
	stop := func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}
	combined := &limitedBuffer{limit: DefaultLimit, stop: stop}
	command.Stdout = combined
	command.Stderr = combined
	err := command.Run()
	output := combined.bytes()
	if combined.wasTruncated() {
		return output, fmt.Errorf("%w after %d bytes: %v", ErrOutputLimit, len(output), err)
	}
	return output, err
}

func outputLimitError(result Result, commandErr error) error {
	streams := "stdout"
	if result.StderrTruncated && !result.StdoutTruncated {
		streams = "stderr"
	} else if result.StdoutTruncated && result.StderrTruncated {
		streams = "stdout and stderr"
	}
	return fmt.Errorf("%w on %s: %v", ErrOutputLimit, streams, commandErr)
}

func (b *limitedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(content)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(content) > remaining {
			content = content[:remaining]
		}
		_, _ = b.buffer.Write(content)
	}
	if original > remaining {
		b.truncated = true
		b.once.Do(b.stop)
	}
	return original, nil
}

func (b *limitedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

func (b *limitedBuffer) wasTruncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
