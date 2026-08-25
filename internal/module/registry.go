package module

import (
	"fmt"
	"sort"
	"sync"
)

// Factory builds a module from its slice of the configuration.
type Factory func(cfg map[string]any) (Module, error)

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

// Register makes a module type available by name. It panics on a duplicate
// name, which can only be a programming error at init time.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[name]; dup {
		panic("module: duplicate registration for " + name)
	}
	factories[name] = f
}

// New builds a module by name.
func New(name string, cfg map[string]any) (Module, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("module: unknown module %q (known: %v)", name, Names())
	}
	return f(cfg)
}

// Names lists every registered module type, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
