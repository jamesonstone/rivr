//go:build darwin || linux

package procidentity

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/subprocess"
)

func Current() (string, error) { return Inspect(os.Getpid()) }

func Inspect(pid int) (string, error) {
	if pid <= 1 {
		return "", fmt.Errorf("invalid PID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := subprocess.Run(exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)))
	if err != nil {
		return "", err
	}
	identity := strings.TrimSpace(string(result.Stdout))
	if identity == "" {
		return "", fmt.Errorf("empty process start identity")
	}
	return identity, nil
}

func Matches(pid int, identity string) bool {
	if identity == "" {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.Signal(0)) != nil {
		return false
	}
	actual, err := Inspect(pid)
	return err == nil && actual == identity
}
