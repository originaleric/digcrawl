package browser

import (
	"strings"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// applyBackgroundLauncherPolicy finalizes a crawler-owned browser launch.
// ExtraArgs remain useful for browser compatibility, but cannot override the
// product invariant that background rendering never emits audio. Interactive
// login intentionally does not call this helper.
func applyBackgroundLauncherPolicy(l *launcher.Launcher, extraArgs []string) *launcher.Launcher {
	l = applyLauncherExtraArgs(l, extraArgs)
	return l.Set(flags.Flag("mute-audio"))
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
