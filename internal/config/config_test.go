package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rbg.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnvOverridesFile(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=fromfile\nRBG_CWD=/proj\n")
	cfg, err := Load(map[string]string{"RBG_HOST": "fromenv"}, conf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "fromenv" {
		t.Errorf("Host = %q, want fromenv", cfg.Host)
	}
	if cfg.CWD != "/proj" {
		t.Errorf("CWD = %q, want /proj", cfg.CWD)
	}
}

func TestSSHOptsSplit(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\nRBG_SSH=-p 2222 -i ~/k\n")
	cfg, _ := Load(map[string]string{}, conf)
	want := []string{"-p", "2222", "-i", "~/k"}
	if !reflect.DeepEqual(cfg.SSHOpts, want) {
		t.Errorf("SSHOpts = %v, want %v", cfg.SSHOpts, want)
	}
}

func TestMissingHostErrors(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "absent.conf")
	if _, err := Load(map[string]string{}, conf); err == nil {
		t.Fatal("expected error for missing RBG_HOST")
	}
}

func TestRelativeCWDErrors(t *testing.T) {
	// A relative RBG_CWD would resolve against the desktop login cwd; reject it
	// early rather than surface a confusing "no working dir" at run time.
	conf := writeConf(t, "RBG_HOST=h\nRBG_CWD=workplace\n")
	if _, err := Load(map[string]string{}, conf); err == nil {
		t.Error("expected error for a relative RBG_CWD")
	}
}

func TestAbsoluteCWDAccepted(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\nRBG_CWD=/home/me\n")
	cfg, err := Load(map[string]string{}, conf)
	if err != nil {
		t.Fatalf("absolute RBG_CWD should load: %v", err)
	}
	if cfg.CWD != "/home/me" {
		t.Errorf("CWD = %q, want /home/me", cfg.CWD)
	}
}

func TestUnsetCWDIsAllowed(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\n")
	cfg, err := Load(map[string]string{}, conf)
	if err != nil {
		t.Fatalf("unset RBG_CWD should load (optional): %v", err)
	}
	if cfg.CWD != "" {
		t.Errorf("CWD = %q, want empty", cfg.CWD)
	}
}

func TestAgentPathDefault(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\n")
	cfg, _ := Load(map[string]string{}, conf)
	if cfg.AgentPath != ".local/bin/rbg-agent" {
		t.Errorf("AgentPath = %q, want default", cfg.AgentPath)
	}
}

func TestQuotedValuesAndComments(t *testing.T) {
	conf := writeConf(t, "# c\nRBG_HOST=\"quoted\"\n\n")
	cfg, _ := Load(map[string]string{}, conf)
	if cfg.Host != "quoted" {
		t.Errorf("Host = %q, want quoted", cfg.Host)
	}
}

func TestMuxDefaultsOn(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\n")
	cfg, _ := Load(map[string]string{}, conf)
	if !cfg.Mux {
		t.Error("Mux should default on")
	}
	if cfg.ControlPath == "" || cfg.ControlPersist == "" {
		t.Errorf("Control defaults missing: path=%q persist=%q", cfg.ControlPath, cfg.ControlPersist)
	}
}

func TestMuxDisabledByEnv(t *testing.T) {
	conf := writeConf(t, "RBG_HOST=h\n")
	cfg, _ := Load(map[string]string{"RBG_MUX": "0"}, conf)
	if cfg.Mux {
		t.Error("RBG_MUX=0 should disable Mux")
	}
}

func TestConfFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rbg.conf")
	in := map[string]string{"RBG_HOST": "h1", "RBG_CWD": "/x", "RBG_MUX": "0"}
	if err := WriteConfFile(p, in); err != nil {
		t.Fatal(err)
	}
	got := ReadConfFileMap(p)
	for k, v := range in {
		if got[k] != v {
			t.Errorf("key %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestWriteConfFilePreservesUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "rbg.conf")
	WriteConfFile(p, map[string]string{"RBG_HOST": "h", "CUSTOM": "keepme"})
	got := ReadConfFileMap(p)
	if got["CUSTOM"] != "keepme" {
		t.Errorf("unknown key dropped: %v", got)
	}
}
