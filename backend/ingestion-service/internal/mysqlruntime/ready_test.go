package mysqlruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublishReadyExclusiveAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready")
	values := map[string]bool{}
	for i := 0; i < 8; i++ {
		values[strings.Repeat(fmt.Sprintf("writer%d", i), 256)] = true
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for v := range values {
		wg.Add(1)
		go func(v string) { defer wg.Done(); results <- PublishReady(path, v) }(v)
	}
	deadline := time.Now().Add(5 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		data, e := os.ReadFile(path)
		if e == nil {
			if !values[string(data)] {
				t.Errorf("partial readiness marker: %d bytes", len(data))
			}
			seen = true
			break
		}
		if !os.IsNotExist(e) {
			t.Error(e)
			break
		}
		time.Sleep(time.Millisecond)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, os.ErrExist) {
			t.Error(e)
		}
	}
	if success != 1 || !seen {
		t.Fatal(success, seen)
	}
}
