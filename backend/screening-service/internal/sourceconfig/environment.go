package sourceconfig

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

var ErrConfiguration = errors.New("invalid_source_database_configuration")

// ReadSecret supports orchestrator-mounted secrets without logging values or
// silently selecting an application/default database. The path is operator config.
func ReadSecret(name string) (string, error) {
	direct, path := os.Getenv(name), os.Getenv(name+"_FILE")
	if direct != "" && path != "" {
		return "", ErrConfiguration
	}
	if path == "" {
		return direct, nil
	}
	file, e := os.Open(path)
	if e != nil {
		return "", ErrConfiguration
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8192 || info.Size() < 1 {
		return "", ErrConfiguration
	}
	raw := make([]byte, info.Size())
	n, e := file.ReadAt(raw, 0)
	if e != nil || n != len(raw) || !utf8.Valid(raw) {
		return "", ErrConfiguration
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", ErrConfiguration
	}
	return value, nil
}
