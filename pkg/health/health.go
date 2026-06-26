package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

type Check func(ctx context.Context) error

type Checker struct {
	mu     sync.RWMutex
	checks map[string]Check
}

func NewChecker() *Checker {
	return &Checker{
		checks: make(map[string]Check),
	}
}

func (c *Checker) Add(name string, check Check) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = check
}

func LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"alive"}`))
	}
}

func ReadyHandler(c *Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.mu.RLock()
		defer c.mu.RUnlock()

		results := make(map[string]string)
		overall := http.StatusOK

		for name, check := range c.checks {
			if err := check(r.Context()); err != nil {
				results[name] = err.Error()
				overall = http.StatusServiceUnavailable
			} else {
				results[name] = "ok"
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(overall)
		json.NewEncoder(w).Encode(map[string]any{
			"status":  results,
			"healthy": overall == http.StatusOK,
		})
	}
}
