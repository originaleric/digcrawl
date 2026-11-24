package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/originaleric/digcrawl/config"
	"github.com/originaleric/digcrawl/internal/browser"
	"github.com/originaleric/digcrawl/transport/bridge"
)

// launchd owns only the foreground watcher. Bridge workers retain their existing
// lifecycle locks and private process groups.
func configureBridgeWatch(profile string, remove bool) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("Bridge watch service installation requires macOS")
	}
	profile, err := browser.CanonicalProfilePath(profile)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Every package installer also holds the standard profile directory lock.
	// Include it so two different executable locations cannot mutate the same
	// profile's LaunchAgent concurrently.
	unique := map[string]bool{}
	for _, resource := range []string{filepath.Dir(executable), filepath.Dir(config.DefaultSkillProfilePath())} {
		canonical, err := browser.CanonicalProfilePath(resource)
		if err != nil {
			return err
		}
		unique[canonical] = true
	}
	resources := make([]string, 0, len(unique))
	for resource := range unique {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	var guards []*browser.ProfileLock
	defer func() {
		for i := len(guards) - 1; i >= 0; i-- {
			guards[i].Close()
		}
	}()
	for _, resource := range resources {
		guard, err := browser.AcquireInstallLock(resource)
		if err != nil {
			return fmt.Errorf("watcher lifecycle conflicts with installation: %w", err)
		}
		guards = append(guards, guard)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(profile))
	label := fmt.Sprintf("com.dig.digcrawl.watch.%x", digest[:12])
	plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	job := bridgeWatchJob{Label: label, plist: plist}
	previousDefinition, readErr := os.ReadFile(plist)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if remove {
		if err := stopWatchJob(ctx, job); err != nil {
			return err
		}
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	dir, err := browser.RuntimeDirectory()
	if err != nil {
		return err
	}
	if err := browser.PrepareRuntimeDirectory(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
		return err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	escape := func(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	environment := "<key>HOME</key><string>" + escape(home) + "</string>"
	// These existing deployment paths select the managed browser files. A
	// launchd child must resolve the same files as the installing CLI, rather
	// than silently falling back to a different HOME/default runtime.
	for _, key := range []string{"XDG_DATA_HOME", "DIGCRAWL_BROWSER_DIR"} {
		if path := strings.TrimSpace(os.Getenv(key)); path != "" {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			environment += "<key>" + key + "</key><string>" + escape(absolute) + "</string>"
		}
	}
	// Watch logs contain bounded controlled messages. Service management keeps
	// launchd's process-restart policy independent of tool execution.
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>bridge</string><string>watch</string><string>--config</string><string>%s</string></array>
<key>WorkingDirectory</key><string>%s</string>
<key>EnvironmentVariables</key><dict>%s</dict>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>ThrottleInterval</key><integer>30</integer>
<key>StandardOutPath</key><string>/dev/null</string>
<key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, escape(label), escape(executable), escape(profile), escape(workingDirectory), environment, escape(filepath.Join(dir, label+".log")))
	if err := stopWatchJob(ctx, job); err != nil {
		if readErr == nil {
			return errors.Join(err, restoreWatchDefinition(job, previousDefinition, true))
		}
		return err
	}
	job.original = []byte(content)
	if err := publishWatchPlist(plist, job.original); err != nil {
		return errors.Join(err, restoreWatchDefinition(job, previousDefinition, readErr == nil))
	}
	if err := startWatchJob(ctx, job); err != nil {
		return errors.Join(err, restoreWatchDefinition(job, previousDefinition, readErr == nil))
	}
	return nil
}

// Publish only complete definitions; installation intent must never be a
// truncated file when launchd or a later installer reads it.
func publishWatchPlist(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".digcrawl-watch-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func restoreWatchDefinition(job bridgeWatchJob, previous []byte, existed bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// A failed bootstrap may still have registered a job. Retire it before
	// restoring its previous definition under the same installation lock.
	if err := stopWatchJob(ctx, job); err != nil {
		return err
	}
	if !existed {
		if err := os.Remove(job.plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := publishWatchPlist(job.plist, previous); err != nil {
		return err
	}
	job.original = previous
	return startWatchJob(ctx, job)
}

// The existing LaunchAgent plist is the installed watcher intent. Installation
// snapshots only jobs bound to its replacement executable, while its existing
// resource locks serialize watch install/uninstall and executable publication.
type bridgeWatchJob struct {
	Label            string   `json:"Label"`
	ProgramArguments []string `json:"ProgramArguments"`
	WorkingDirectory string   `json:"WorkingDirectory"`
	plist            string
	original         []byte
}

func watchJobState(ctx context.Context, label string) (bool, int, error) {
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + label
	output, err := exec.CommandContext(ctx, "launchctl", "print", target).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return false, 0, ctx.Err()
		}
		if bytes.Contains(output, []byte("Could not find service")) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("inspect watcher %s: %w", label, err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "pid" && fields[1] == "=" {
			pid, err := strconv.Atoi(fields[2])
			return true, pid, err
		}
	}
	return true, 0, nil
}

func stopWatchJob(ctx context.Context, job bridgeWatchJob) error {
	exists, pid, err := watchJobState(ctx, job.Label)
	if err != nil || !exists {
		return err
	}
	identity := ""
	if pid > 0 {
		identity, err = browser.ProcessIdentity(ctx, pid)
		if err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + job.Label
	if output, err := exec.CommandContext(ctx, "launchctl", "bootout", target).CombinedOutput(); err != nil {
		return fmt.Errorf("stop watcher %s: %w (%s)", job.Label, err, strings.TrimSpace(string(output)))
	}
	for pid > 0 && identity != "" {
		current, err := browser.ProcessIdentity(ctx, pid)
		if errors.Is(err, syscall.ESRCH) || (err == nil && current != identity) {
			break
		}
		if err != nil {
			return err
		}
		alive, err := browser.ProcessAlive(pid)
		if err != nil {
			return fmt.Errorf("wait for watcher %s PID %d: %w", job.Label, pid, err)
		}
		if !alive {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return nil
}

func startWatchJob(ctx context.Context, job bridgeWatchJob) error {
	data, err := os.ReadFile(job.plist)
	if os.IsNotExist(err) {
		return nil
	} // explicit uninstall intent wins
	if err != nil {
		return err
	}
	if !bytes.Equal(data, job.original) {
		return fmt.Errorf("watcher definition changed during installation: %s", job.Label)
	}
	exists, _, err := watchJobState(ctx, job.Label)
	if err != nil || exists {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if output, err := exec.CommandContext(ctx, "launchctl", "bootstrap", domain, job.plist).CombinedOutput(); err != nil {
		return fmt.Errorf("restore watcher %s: %w (%s)", job.Label, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func installedWatchJobs(ctx context.Context, executable string) ([]bridgeWatchJob, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(home, "Library", "LaunchAgents")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if executable != "" {
		executable, err = browser.CanonicalProfilePath(executable)
		if err != nil {
			return nil, err
		}
	}
	var jobs []bridgeWatchJob
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "com.dig.digcrawl.watch.") || !strings.HasSuffix(entry.Name(), ".plist") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		output, err := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", path).Output()
		if err != nil {
			return nil, fmt.Errorf("read watcher definition %s: %w", entry.Name(), err)
		}
		var job bridgeWatchJob
		if err := json.Unmarshal(output, &job); err != nil {
			return nil, err
		}
		if len(job.ProgramArguments) == 0 {
			return nil, fmt.Errorf("watcher has no program: %s", entry.Name())
		}
		program, err := browser.CanonicalProfilePath(job.ProgramArguments[0])
		if err != nil {
			return nil, err
		}
		if executable != "" && program != executable {
			continue
		}
		if len(job.ProgramArguments) != 5 || job.ProgramArguments[1] != "bridge" || job.ProgramArguments[2] != "watch" || job.ProgramArguments[3] != "--config" {
			return nil, fmt.Errorf("unsupported watcher definition %s", entry.Name())
		}
		profile, err := browser.CanonicalProfilePath(job.ProgramArguments[4])
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256([]byte(profile))
		expected := fmt.Sprintf("com.dig.digcrawl.watch.%x", digest[:12])
		if job.Label != expected || entry.Name() != expected+".plist" {
			return nil, fmt.Errorf("watcher profile identity mismatch: %s", entry.Name())
		}
		if !filepath.IsAbs(job.WorkingDirectory) {
			return nil, fmt.Errorf("watcher has no absolute working directory; reinstall it from the intended directory: %s", entry.Name())
		}
		job.ProgramArguments[4] = profile
		job.plist = path
		job.original, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func suspendInstalledWatchers(parent context.Context, executable string) (func() error, error) {
	ctx := parent
	var jobs []bridgeWatchJob
	if runtime.GOOS == "darwin" {
		var err error
		jobs, err = installedWatchJobs(ctx, executable)
		if err != nil {
			return nil, err
		}
	}
	var releases []func()
	profiles := map[string]string{}
	// Keep the same stop intents over the complete publication, not just a
	// single stop command. Durable operator pause remains independent.
	for _, job := range jobs {
		profiles[job.ProgramArguments[4]] = job.WorkingDirectory
	}
	if _, err := os.Stat(config.DefaultSkillProfilePath()); err == nil {
		profile, err := browser.CanonicalProfilePath(config.DefaultSkillProfilePath())
		if err != nil {
			return nil, err
		}
		if _, exists := profiles[profile]; !exists {
			profiles[profile] = ""
		}
	}
	restore := func() error {
		// Publication/configuration work is complete (or has been aborted).
		// Remove our admission block before loading passive watchers, so an
		// installed watcher can cold-start immediately rather than one tick later.
		for _, release := range releases {
			release()
		}
		releases = nil
		restoreCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var failures []error
		for _, job := range jobs {
			if err := startWatchJob(restoreCtx, job); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	for profile := range profiles {
		release, err := bridge.HoldStop(profile)
		if err != nil {
			return nil, errors.Join(err, restore())
		}
		releases = append(releases, release)
	}
	for _, job := range jobs {
		stopping, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := stopWatchJob(stopping, job)
		cancel()
		if err != nil {
			return nil, errors.Join(err, restore())
		}
	}
	// Include custom watcher profiles, rather than only the standard Skill
	// profile. HoldStop prevents their MCP clients racing executable publication.
	standard, err := browser.CanonicalProfilePath(config.DefaultSkillProfilePath())
	if err != nil {
		return nil, errors.Join(err, restore())
	}
	for profile, directory := range profiles {
		if _, err := bridge.StopInDirectory(ctx, profile, directory); err != nil {
			return nil, errors.Join(err, restore())
		}
		if profile == standard {
			if err := config.MigrateBrowserRuntimeProfile(ctx, profile); err != nil {
				return nil, errors.Join(err, restore())
			}
		}
	}

	return restore, nil
}

// Manual profile preparation and package preparation use the same recorded
// launch context. No directory is recomputed in the parent package installers.
func watcherProfileDirectory(ctx context.Context, profile string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", nil
	}
	profile, err := browser.CanonicalProfilePath(profile)
	if err != nil {
		return "", err
	}
	jobs, err := installedWatchJobs(ctx, "")
	if err != nil {
		return "", err
	}
	for _, job := range jobs {
		if job.ProgramArguments[4] == profile {
			return job.WorkingDirectory, nil
		}
	}
	return "", nil
}
