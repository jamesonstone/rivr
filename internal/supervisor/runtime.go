//go:build darwin || linux

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jamesonstone/rungrid/internal/errs"
	"github.com/jamesonstone/rungrid/internal/processcompose"
	"github.com/jamesonstone/rungrid/internal/state"
)

type Runtime struct {
	APIVersion            string `json:"api_version"`
	ProjectID             string `json:"project_id"`
	GenerationID          string `json:"generation_id"`
	PID                   int    `json:"pid"`
	ProcessIdentity       string `json:"process_identity"`
	ProcessCommand        string `json:"process_command"`
	Socket                string `json:"socket"`
	SocketDevice          uint64 `json:"socket_device"`
	SocketInode           uint64 `json:"socket_inode"`
	ProcessCompose        string `json:"process_compose"`
	ProcessComposeVersion string `json:"process_compose_version"`
	Configuration         string `json:"configuration"`
	ConfigurationHash     string `json:"configuration_hash"`
	WorkspaceRoot         string `json:"workspace_root"`
	StartedAt             string `json:"started_at"`
}

type StartOptions struct {
	Layout                state.Layout
	GenerationID          string
	WorkspaceRoot         string
	ProcessCompose        string
	ProcessComposeVersion string
	RungridExecutable     string
	StartupTimeout        time.Duration
}

func Start(ctx context.Context, options StartOptions) (result Runtime, reused bool, returnErr error) {
	if err := options.Layout.Ensure(); err != nil {
		return Runtime{}, false, err
	}
	if existing, err := Read(options.Layout); err == nil {
		if existing.GenerationID != options.GenerationID {
			return Runtime{}, false, errs.New(errs.ExitConflict, "RG601", "a different generated runtime is active; run rungrid down first")
		}
		if err := Verify(ctx, options.Layout, existing); err != nil {
			return Runtime{}, false, err
		}
		return existing, true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Runtime{}, false, err
	}

	generationDirectory := filepath.Join(options.Layout.ProjectDir, "generations", options.GenerationID)
	configuration := filepath.Join(generationDirectory, "process-compose.yaml")
	configurationContent, err := os.ReadFile(configuration)
	if err != nil {
		return Runtime{}, false, errs.Wrap(errs.ExitConflict, "RG602", "read generated Process Compose configuration", err)
	}
	socket := filepath.Join(options.Layout.ProjectDir, "runtime.sock")
	if _, err := os.Lstat(socket); err == nil {
		return Runtime{}, false, errs.New(errs.ExitConflict, "RG604", "an unverified runtime socket already exists; run rungrid doctor")
	} else if !os.IsNotExist(err) {
		return Runtime{}, false, errs.Wrap(errs.ExitConflict, "RG605", "inspect runtime socket", err)
	}
	serverLog := filepath.Join(options.Layout.ProjectDir, "process-compose.log")
	arguments := []string{
		"-D",
		"-t=false",
		"-U",
		"-u", filepath.Join("..", "..", "runtime.sock"),
		"-f", configuration,
		"--keep-project",
		"--ordered-shutdown",
		"--disable-dotenv",
		"-L", serverLog,
	}
	command := exec.CommandContext(ctx, options.ProcessCompose, arguments...)
	command.Dir = generationDirectory
	command.Env = processcompose.EnvironmentWithRuntime(os.Environ(), options.RungridExecutable, options.Layout.StateRoot, options.WorkspaceRoot, options.GenerationID)
	output, err := command.CombinedOutput()
	if err != nil {
		message := "start detached Process Compose runtime"
		if len(strings.TrimSpace(string(output))) > 0 {
			message += " (subprocess output redacted)"
		}
		return Runtime{}, false, errs.Wrap(errs.ExitDependency, "RG606", message, err)
	}
	launched := true
	defer func() {
		if !launched || returnErr == nil {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupClient := processcompose.Client{
			Executable: options.ProcessCompose,
			Socket:     "runtime.sock",
			LogFile:    processcompose.ClientLog(options.Layout.ProjectDir),
			WorkDir:    options.Layout.ProjectDir,
		}
		if cleanupClient.Down(cleanupContext) == nil {
			if _, _, uid, socketErr := inspectSocket(socket); socketErr == nil && uid == uint32(os.Getuid()) {
				_ = os.Remove(socket)
			}
			_ = os.Remove(filepath.Join(options.Layout.ProjectDir, "runtime.json"))
		}
	}()
	waitContext, cancel := context.WithTimeout(ctx, options.StartupTimeout)
	defer cancel()
	if err := waitForSocket(waitContext, socket); err != nil {
		return Runtime{}, false, err
	}
	pid, err := socketPID(waitContext, configuration)
	if err != nil {
		return Runtime{}, false, err
	}
	processIdentity, processCommand, err := inspectProcess(waitContext, pid)
	if err != nil {
		return Runtime{}, false, err
	}
	device, inode, uid, err := inspectSocket(socket)
	if err != nil {
		return Runtime{}, false, err
	}
	if uid != uint32(os.Getuid()) {
		return Runtime{}, false, errs.New(errs.ExitConflict, "RG607", "runtime socket is not owned by the current user")
	}
	runtimeState := Runtime{
		APIVersion:            "rungrid/output/v1",
		ProjectID:             options.Layout.ProjectID,
		GenerationID:          options.GenerationID,
		PID:                   pid,
		ProcessIdentity:       processIdentity,
		ProcessCommand:        processCommand,
		Socket:                socket,
		SocketDevice:          device,
		SocketInode:           inode,
		ProcessCompose:        options.ProcessCompose,
		ProcessComposeVersion: options.ProcessComposeVersion,
		Configuration:         configuration,
		ConfigurationHash:     state.Hash(configurationContent),
		WorkspaceRoot:         options.WorkspaceRoot,
		StartedAt:             state.RuntimeTimestamp(),
	}
	if err := Write(options.Layout, runtimeState); err != nil {
		return Runtime{}, false, err
	}
	client := Client(options.Layout, runtimeState)
	pingContext, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingContext); err != nil {
		return Runtime{}, false, errs.Wrap(errs.ExitConflict, "RG608", "detached Process Compose runtime did not answer on its recorded socket", err)
	}
	launched = false
	return runtimeState, false, nil
}

func Read(layout state.Layout) (Runtime, error) {
	filename := filepath.Join(layout.ProjectDir, "runtime.json")
	info, err := os.Lstat(filename)
	if err != nil {
		return Runtime{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return Runtime{}, errs.New(errs.ExitConflict, "RG609", "runtime record is not a private regular file")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		return Runtime{}, errs.Wrap(errs.ExitConflict, "RG610", "read runtime record", err)
	}
	var result Runtime
	if err := json.Unmarshal(content, &result); err != nil {
		return Runtime{}, errs.Wrap(errs.ExitConflict, "RG611", "decode runtime record", err)
	}
	return result, nil
}

func Write(layout state.Layout, runtimeState Runtime) error {
	content, err := json.MarshalIndent(runtimeState, "", "  ")
	if err != nil {
		return errs.Wrap(errs.ExitFailure, "RG612", "encode runtime record", err)
	}
	return state.WriteFileAtomic(layout.ProjectDir, "runtime.json", append(content, '\n'), 0o600)
}

func Verify(ctx context.Context, layout state.Layout, runtimeState Runtime) error {
	if runtimeState.APIVersion != "rungrid/output/v1" || runtimeState.ProjectID != layout.ProjectID {
		return errs.New(errs.ExitConflict, "RG613", "runtime record belongs to another project or API")
	}
	expectedSocket := filepath.Join(layout.ProjectDir, "runtime.sock")
	if filepath.Clean(runtimeState.Socket) != expectedSocket {
		return errs.New(errs.ExitConflict, "RG614", "runtime socket path is outside the selected project state")
	}
	identity, command, err := inspectProcess(ctx, runtimeState.PID)
	if err != nil {
		return errs.Wrap(errs.ExitConflict, "RG615", "runtime PID is stale", err)
	}
	if identity != runtimeState.ProcessIdentity || command != runtimeState.ProcessCommand {
		return errs.New(errs.ExitConflict, "RG616", "runtime PID identity no longer matches")
	}
	device, inode, uid, err := inspectSocket(runtimeState.Socket)
	if err != nil {
		return err
	}
	if uid != uint32(os.Getuid()) || device != runtimeState.SocketDevice || inode != runtimeState.SocketInode {
		return errs.New(errs.ExitConflict, "RG617", "runtime Unix socket identity no longer matches")
	}
	configuration, err := os.ReadFile(runtimeState.Configuration)
	if err != nil || state.Hash(configuration) != runtimeState.ConfigurationHash {
		return errs.New(errs.ExitConflict, "RG618", "active Process Compose configuration was modified")
	}
	pingContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := Client(layout, runtimeState).Ping(pingContext); err != nil {
		return errs.Wrap(errs.ExitConflict, "RG619", "runtime socket does not answer as the recorded Process Compose server", err)
	}
	return nil
}

func Client(layout state.Layout, runtimeState Runtime) processcompose.Client {
	return processcompose.Client{
		Executable: runtimeState.ProcessCompose,
		Socket:     "runtime.sock",
		LogFile:    processcompose.ClientLog(layout.ProjectDir),
		WorkDir:    layout.ProjectDir,
	}
}

func Stop(ctx context.Context, layout state.Layout, runtimeState Runtime) error {
	if err := Verify(ctx, layout, runtimeState); err != nil {
		return err
	}
	client := Client(layout, runtimeState)
	if err := client.Down(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for processExists(runtimeState.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processExists(runtimeState.PID) {
		return errs.New(errs.ExitPartial, "RG620", "Process Compose did not exit after ordered shutdown")
	}
	if info, err := os.Lstat(runtimeState.Socket); err == nil {
		device, inode, uid, statErr := inspectSocket(runtimeState.Socket)
		if statErr != nil || uid != uint32(os.Getuid()) || device != runtimeState.SocketDevice || inode != runtimeState.SocketInode || info.Mode()&os.ModeSocket == 0 {
			return errs.New(errs.ExitConflict, "RG621", "refusing to remove a socket whose identity changed")
		}
		if err := os.Remove(runtimeState.Socket); err != nil {
			return errs.Wrap(errs.ExitPartial, "RG622", "remove stopped runtime socket", err)
		}
	} else if !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitPartial, "RG623", "inspect stopped runtime socket", err)
	}
	if err := os.Remove(filepath.Join(layout.ProjectDir, "runtime.json")); err != nil && !os.IsNotExist(err) {
		return errs.Wrap(errs.ExitPartial, "RG624", "remove stopped runtime record", err)
	}
	return nil
}

func waitForSocket(ctx context.Context, socket string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errs.Wrap(errs.ExitNotReady, "RG625", "wait for Process Compose Unix socket", ctx.Err())
		case <-ticker.C:
		}
	}
}

func socketPID(ctx context.Context, configuration string) (int, error) {
	output, err := exec.CommandContext(ctx, "lsof", "-nP", "-U", "-F", "pcn").Output()
	if err != nil {
		return 0, errs.Wrap(errs.ExitDependency, "RG626", "identify Process Compose socket owner with lsof", err)
	}
	pid := 0
	commandName := ""
	for _, line := range strings.Split(string(output), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			commandName = ""
		case 'c':
			commandName = line[1:]
		case 'n':
			if pid <= 1 || !strings.Contains(strings.ToLower(commandName), "process-c") || filepath.Clean(line[1:]) != filepath.Join("..", "..", "runtime.sock") {
				continue
			}
			_, processCommand, inspectErr := inspectProcess(ctx, pid)
			if inspectErr == nil && strings.Contains(processCommand, configuration) {
				return pid, nil
			}
		}
	}
	return 0, errs.New(errs.ExitConflict, "RG627", "no process owns the Process Compose socket")
}

func inspectProcess(ctx context.Context, pid int) (string, string, error) {
	if pid <= 1 || !processExists(pid) {
		return "", "", errs.New(errs.ExitConflict, "RG628", "runtime process does not exist")
	}
	identityOutput, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", "", errs.Wrap(errs.ExitConflict, "RG629", "inspect runtime process start identity", err)
	}
	commandOutput, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", "", errs.Wrap(errs.ExitConflict, "RG630", "inspect runtime process command", err)
	}
	identity := strings.TrimSpace(string(identityOutput))
	command := strings.TrimSpace(string(commandOutput))
	if identity == "" || command == "" || !strings.Contains(strings.ToLower(command), "process-compose") {
		return "", "", errs.New(errs.ExitConflict, "RG631", "runtime PID is not Process Compose")
	}
	return identity, command, nil
}

func inspectSocket(filename string) (uint64, uint64, uint32, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return 0, 0, 0, errs.Wrap(errs.ExitConflict, "RG632", "inspect runtime Unix socket", err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return 0, 0, 0, errs.New(errs.ExitConflict, "RG633", "runtime path is not a Unix socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, errs.New(errs.ExitConflict, "RG634", "Unix socket identity is unavailable")
	}
	return uint64(stat.Dev), uint64(stat.Ino), stat.Uid, nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
