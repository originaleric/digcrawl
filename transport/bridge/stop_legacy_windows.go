//go:build windows

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
)

type windowsBridgeProcess struct {
	PID        int    `json:"ProcessId"`
	Executable string `json:"ExecutablePath"`
	Created    string `json:"CreationDate"`
	Command    string `json:"CommandLine"`
}

// Windows uses the private HTTP shutdown request for task draining. If that
// socket is unreachable, identify the owner for an explicit error; forcibly
// terminating its tree cannot establish that current tasks have finished.
func stopBridgeOwner(ctx context.Context, socket string) (bool, error) {
	processes, err := listWindowsBridgeProcesses(ctx)
	if err != nil {
		return false, err
	}
	for _, candidate := range processes {
		pid := candidate.PID
		if pid <= 0 || !windowsIsBridgeCommand(candidate) || !windowsHasSocketArgument(candidate.Command, socket) {
			continue
		}

		// Re-enumerate before reporting a live owner and reject PID reuse.
		current, err := listWindowsBridgeProcesses(ctx)
		if err != nil {
			return false, err
		}
		verified := false
		for _, p := range current {
			if p.PID == pid && p.Created != "" && p.Created == candidate.Created && p.Executable == candidate.Executable && p.Command == candidate.Command {
				verified = true
				break
			}
		}
		if !verified {
			continue
		}

		return false, fmt.Errorf("live Bridge is unreachable; cannot confirm current tasks have finished; restore its connection before stopping")
	}
	return false, ErrBridgeOwnerNotFound
}

func listWindowsBridgeProcesses(ctx context.Context) ([]windowsBridgeProcess, error) {
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-CimInstance Win32_Process | Select-Object ProcessId,CommandLine,ExecutablePath,CreationDate | ConvertTo-Json -Compress").Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("inspect local bridge processes: %w", err)
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "{") {
		var process windowsBridgeProcess
		if err := json.Unmarshal([]byte(trimmed), &process); err != nil {
			return nil, fmt.Errorf("decode local bridge process: %w", err)
		}
		return []windowsBridgeProcess{process}, nil
	}
	var processes []windowsBridgeProcess
	if err := json.Unmarshal([]byte(trimmed), &processes); err != nil {
		return nil, fmt.Errorf("decode local bridge processes: %w", err)
	}
	return processes, nil
}

func windowsHasSocketArgument(command, socket string) bool {
	args, err := windows.DecomposeCommandLine(command)
	if err != nil {
		return false
	}
	for i, arg := range args {
		if arg == "--socket" && i+1 < len(args) && args[i+1] == socket {
			return true
		}
		if strings.HasPrefix(arg, "--socket=") && strings.TrimPrefix(arg, "--socket=") == socket {
			return true
		}
	}
	return false
}

func stopLegacyBridgeCandidates(ctx context.Context, socket string) (bool, error) {
	return stopBridgeOwner(ctx, socket)
}

func windowsIsBridgeCommand(p windowsBridgeProcess) bool {
	if p.Created == "" || !isBridgeExecutable(p.Executable) {
		return false
	}
	args, err := windows.DecomposeCommandLine(p.Command)
	return err == nil && len(args) >= 3 && args[1] == "bridge" && args[2] == "serve" && isBridgeExecutable(args[0])
}

func InspectOwners(ctx context.Context, profile string) ([]OwnerDiagnostic, error) {
	socket, err := SocketPath(profile)
	if err != nil {
		return nil, err
	}
	owners, err := inspectSocketOwners(ctx, socket)
	if err != nil {
		return nil, err
	}
	legacy, err := legacySocketPathForSocket(socket)
	if err != nil {
		return nil, err
	}
	if legacy != socket {
		previous, err := inspectSocketOwners(ctx, legacy)
		if err != nil {
			return nil, err
		}
		owners = append(owners, previous...)
	}
	return owners, nil
}
func inspectSocketOwners(ctx context.Context, socket string) ([]OwnerDiagnostic, error) {
	processes, err := listWindowsBridgeProcesses(ctx)
	if err != nil {
		return nil, err
	}
	var result []OwnerDiagnostic
	for _, p := range processes {
		if p.PID == os.Getpid() || !windowsHasSocketArgument(p.Command, socket) || !strings.Contains(p.Command, "bridge serve") {
			continue
		}
		result = append(result, OwnerDiagnostic{PID: p.PID, Executable: p.Executable, Verified: windowsIsBridgeCommand(p)})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
