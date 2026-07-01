// Package config loads rbg client configuration from environment variables
// (which win) layered over a ~/.rbg.conf KEY=value file.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config is the resolved client configuration.
type Config struct {
	Host           string
	CWD            string
	SSHOpts        []string
	AgentPath      string
	Mux            bool   // reuse one SSH connection across commands (multiplexing)
	ControlPath    string // ssh ControlPath socket (when Mux)
	ControlPersist string // ssh ControlPersist idle duration (when Mux)
}

const (
	defaultAgentPath      = ".local/bin/rbg-agent"
	defaultControlPath    = "~/.rbg/cm-%C" // %C = hash of (host,port,user)
	defaultControlPersist = "10m"
)

// Load merges env over the conf file at confPath. Pass os.Environ-derived map
// for env; a missing file is not an error (only a missing RBG_HOST is).
func Load(env map[string]string, confPath string) (*Config, error) {
	fileVals := readConfFile(confPath)
	get := func(key string) string {
		if v, ok := env[key]; ok && v != "" {
			return v
		}
		return fileVals[key]
	}

	host := get("RBG_HOST")
	if host == "" {
		return nil, errors.New("RBG_HOST not set (export it or put it in ~/.rbg.conf)")
	}
	cwd := get("RBG_CWD")
	if cwd != "" && !filepath.IsAbs(cwd) {
		// CWD roots the desktop's working dir and the repo→dir derivation; a
		// relative value would resolve against the login shell's cwd on the
		// desktop, so reject it early with a clear message rather than let it
		// surface later as a confusing "no working dir" at run time.
		return nil, fmt.Errorf("RBG_CWD must be an absolute path, got %q", cwd)
	}
	agentPath := get("RBG_AGENT_PATH")
	if agentPath == "" {
		agentPath = defaultAgentPath
	}
	mux := true
	switch strings.ToLower(get("RBG_MUX")) {
	case "0", "false", "no", "off":
		mux = false
	}
	controlPath := get("RBG_CONTROL_PATH")
	if controlPath == "" {
		controlPath = defaultControlPath
	}
	controlPersist := get("RBG_CONTROL_PERSIST")
	if controlPersist == "" {
		controlPersist = defaultControlPersist
	}
	return &Config{
		Host:           host,
		CWD:            cwd,
		SSHOpts:        strings.Fields(get("RBG_SSH")),
		AgentPath:      agentPath,
		Mux:            mux,
		ControlPath:    controlPath,
		ControlPersist: controlPersist,
	}, nil
}

// ReadConfFileMap returns the KEY=value pairs in the conf file (empty if absent).
func ReadConfFileMap(path string) map[string]string {
	return readConfFile(path)
}

// WriteConfFile writes vals as sorted KEY=value lines, creating parent dirs.
// It overwrites the file; callers should pass the full desired key set (read,
// merge, write) so unrelated keys are preserved.
func WriteConfFile(path string, vals map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, vals[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func readConfFile(path string) map[string]string {
	vals := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return vals
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		vals[strings.TrimSpace(k)] = v
	}
	return vals
}
