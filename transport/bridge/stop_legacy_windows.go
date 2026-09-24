//go:build windows

package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type windowsBridgeProcess struct {
	PID     int    `json:"ProcessId"`
	Command string `json:"CommandLine"`
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
	pid := 0
	for _, process := range processes {
		if process.PID <= 0 || !strings.Contains(process.Command, "bridge serve") || !windowsHasSocketArgument(process.Command, socket) {
			continue
		}
		pid = process.PID
		break
	}
	if pid == 0 {
		return false, ErrBridgeOwnerNotFound
	}
	output, err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").CombinedOutput()
	if err != nil && ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "not found") {
		return false, fmt.Errorf("force-stop local bridge process tree: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return true, nil
}

func listWindowsBridgeProcesses(ctx context.Context) ([]windowsBridgeProcess, error) {
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-CimInstance Win32_Process | Select-Object ProcessId,CommandLine | ConvertTo-Json -Compress").Output()
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
	for _, needle := range []string{"--socket " + socket, "--socket \"" + socket + "\""} {
		if strings.Contains(command, needle+" ") || strings.HasSuffix(command, needle) {
			return true
		}
	}
	return false
}
