//go:build windows

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type windowsBridgeProcess struct {
	PID        int    `json:"ProcessId"`
	Executable string `json:"ExecutablePath"`
	Created    string `json:"CreationDate"`
	Command    string `json:"CommandLine"`
}

// stopBridgeOwner is the Windows counterpart to Unix's private process group.
// taskkill /T terminates the exact Bridge process and the Chromium tree it
// owns. Process selection still requires the full generated socket argument;
// it never targets a broad `digcrawl` image name.
func stopBridgeOwner(ctx context.Context, socket string) (bool, error) {
	processes, err := listWindowsBridgeProcesses(ctx)
	if err != nil {
		return false, err
	}
	stopped := false
	for _, candidate := range processes {
		pid := candidate.PID
		if pid <= 0 || !windowsIsBridgeCommand(candidate) || !windowsHasSocketArgument(candidate.Command, socket) {
			continue
		}

		// Re-enumerate immediately before signalling and reject PID reuse.
		current, err := listWindowsBridgeProcesses(ctx)
		if err != nil {
			return stopped, err
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

		output, err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput()
		if err != nil && ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil && !strings.Contains(strings.ToLower(string(output)), "not found") {
			return false, fmt.Errorf("force-stop local bridge process tree: %w: %s", err, strings.TrimSpace(string(output)))
		}
		stopped = true
	}
	if !stopped {
		return false, ErrBridgeOwnerNotFound
	}
	return stopped, nil
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
	return inspectSocketOwners(ctx, socket)
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
