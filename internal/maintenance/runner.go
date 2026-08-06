package maintenance

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jamesonstone/rungrid/internal/subprocess"
)

type Runner interface {
	Run(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error)
}

type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	result, err := subprocess.Run(command)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", executable, err)
	}
	return result.Stdout, nil
}

func git(ctx context.Context, runner Runner, directory string, arguments ...string) ([]byte, error) {
	return runner.Run(ctx, directory, "git", arguments...)
}

func gitText(ctx context.Context, runner Runner, directory string, arguments ...string) (string, error) {
	content, err := git(ctx, runner, directory, arguments...)
	return strings.TrimSpace(string(content)), err
}
