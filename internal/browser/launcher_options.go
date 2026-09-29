package browser

import (
	"strings"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/originaleric/digcrawl/config"
)

// applyBackgroundLauncherPolicy finalizes a crawler-owned browser launch.
// ExtraArgs remain useful for headless compatibility, but cannot override the
// product invariant that background rendering never emits audio.
func applyBackgroundLauncherPolicy(l *launcher.Launcher, extraArgs []string) *launcher.Launcher {
	l = applyLauncherExtraArgs(l, extraArgs)
	return l.Set(flags.Flag("mute-audio"))
}

// applyInteractiveLauncherPolicy creates a distinct boundary for user-facing
// login Chrome. In particular, it must not inherit ExtraArgs: configurations
// that make a headless shell work (such as --single-process or --no-zygote)
// change Chrome's process model and are not safe defaults for visible input.
// Sandbox disabling is likewise opt-in through InteractiveExtraArgs only.
func applyInteractiveLauncherPolicy(l *launcher.Launcher, cfg config.LocalBrowserConfig) *launcher.Launcher {
	l = l.NoSandbox(false)
	return applyLauncherExtraArgs(l, cfg.InteractiveExtraArgs)
}

func applyLauncherExtraArgs(l *launcher.Launcher, args []string) *launcher.Launcher {
	for _, raw := range args {
		arg := strings.TrimSpace(raw)
		if arg == "" {
			continue
		}
		arg = strings.TrimPrefix(arg, "--")
		name, value, hasValue := strings.Cut(arg, "=")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if hasValue {
			l = l.Set(flags.Flag(name), value)
			continue
		}
		l = l.Set(flags.Flag(name))
	}
	return l
}
