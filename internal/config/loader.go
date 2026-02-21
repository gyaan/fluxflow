package config

import (
	"fmt"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Loader reads a YAML config file and watches it for changes.
type Loader struct {
	path     string
	mu       sync.RWMutex
	current  *RuleConfig
	onChange []func(*RuleConfig)
	watcher  *fsnotify.Watcher
}

// NewLoader creates a Loader and performs the initial load.
func NewLoader(path string) (*Loader, error) {
	l := &Loader{path: path}
	cfg, err := l.load()
	if err != nil {
		return nil, err
	}
	l.current = cfg
	return l, nil
}

// Config returns the current (latest) configuration.
func (l *Loader) Config() *RuleConfig {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// OnChange registers a callback invoked whenever the config reloads.
func (l *Loader) OnChange(fn func(*RuleConfig)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onChange = append(l.onChange, fn)
}

// Watch starts a background goroutine that hot-reloads the config on file changes.
// Call the returned stop function to clean up.
func (l *Loader) Watch() (stop func(), err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("config watcher: %w", err)
	}
	if err := w.Add(l.path); err != nil {
		w.Close()
		return nil, fmt.Errorf("config watcher add %s: %w", l.path, err)
	}
	l.watcher = w

	done := make(chan struct{})
	go func() {
		defer w.Close()
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Has(fsnotify.Write) || ev.Has(fsnotify.Create) {
					cfg, err := l.load()
					if err != nil {
						// Log and continue with old config.
						continue
					}
					l.mu.Lock()
					l.current = cfg
					callbacks := make([]func(*RuleConfig), len(l.onChange))
					copy(callbacks, l.onChange)
					l.mu.Unlock()
					for _, fn := range callbacks {
						fn(cfg)
					}
				}
			case <-w.Errors:
				// Ignore watcher errors.
			case <-done:
				return
			}
		}
	}()

	return func() { close(done) }, nil
}

// Reload forces an immediate re-read of the config file.
func (l *Loader) Reload() (*RuleConfig, error) {
	cfg, err := l.load()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.current = cfg
	callbacks := make([]func(*RuleConfig), len(l.onChange))
	copy(callbacks, l.onChange)
	l.mu.Unlock()
	for _, fn := range callbacks {
		fn(cfg)
	}
	return cfg, nil
}

func (l *Loader) load() (*RuleConfig, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", l.path, err)
	}
	var cfg RuleConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", l.path, err)
	}
	// Apply defaults.
	if cfg.Engine.EventWorkers == 0 {
		cfg.Engine.EventWorkers = 32
	}
	if cfg.Engine.ActionWorkers == 0 {
		cfg.Engine.ActionWorkers = 16
	}
	if cfg.Engine.QueueDepth == 0 {
		cfg.Engine.QueueDepth = 10000
	}
	if cfg.Engine.EventTimeoutMs == 0 {
		cfg.Engine.EventTimeoutMs = 5000
	}
	return &cfg, nil
}
