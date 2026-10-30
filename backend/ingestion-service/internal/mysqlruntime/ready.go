package mysqlruntime

import (
	"errors"
	"os"
	"path/filepath"
)

// PublishReady atomically exposes a complete marker without replacing an existing
// one. The same-directory temporary file keeps hard-link publication local.
func PublishReady(path, value string) (result error) {
	if path == "" || value == "" || len(value) > 4096 {
		return errors.New("bounded readiness path/value required")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".ingestion-ready-*")
	if e != nil {
		return e
	}
	defer func() {
		if cleanup := os.Remove(f.Name()); cleanup != nil {
			result = errors.Join(result, cleanup)
		}
	}()
	if _, e = f.WriteString(value); e != nil {
		return errors.Join(e, f.Close())
	}
	if e = f.Sync(); e != nil {
		return errors.Join(e, f.Close())
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Link(f.Name(), path)
}
