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
