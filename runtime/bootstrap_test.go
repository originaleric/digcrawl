package runtime

import (
	"testing"

	"github.com/originaleric/digcrawl/config"
)

func TestDefaultSkillProfileRegistersEnableToolWhenRootASRProviderIsDisabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	disabled := false
	cfg := &config.Config{SourcePath: config.DefaultSkillProfilePath(), Providers: config.ProvidersConfig{ASREnabled: &disabled}}
	reg, err := NewRegistry(cfg, RegistryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("skill.asr.enable"); !ok {
		t.Fatal("standard Skill profile must retain its configuration tool")
	}
	if _, ok := reg.Get("asr.transcribe"); ok {
		t.Fatal("an explicitly disabled ASR provider must not restore asr.transcribe")
	}
}
