// Package config loads rbg client configuration from environment variables
// (which win) layered over a ~/.rbg.conf KEY=value file.
package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// Config is the resolved client configuration.
type Config struct {
	Host      string
	CWD       string
	SSHOpts   []string
	AgentPath string
}

const defaultAgentPath = "~/.local/bin/rbg-agent"

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
	agentPath := get("RBG_AGENT_PATH")
	if agentPath == "" {
		agentPath = defaultAgentPath
	}
	return &Config{
		Host:      host,
		CWD:       get("RBG_CWD"),
		SSHOpts:   strings.Fields(get("RBG_SSH")),
		AgentPath: agentPath,
	}, nil
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
