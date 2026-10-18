package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadDotenv reads a KEY=VALUE file into the process environment, without
// overriding variables that are already set: a real environment (docker
// compose, CI secrets) always wins over the file.
//
// It exists so `make run` works on the host: docker compose injects .env
// into containers, but a bare `go run` has nothing to inject. The parser
// is deliberately minimal — no export prefixes, no variable expansion.
func loadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // env-only mode: containers, CI
		}
		return fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue // not an assignment, skip silently
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip one pair of surrounding quotes.
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue // existing environment wins
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: set %s: %w", key, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	return nil
}
