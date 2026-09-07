package sandbox

import "strings"

// Registry mirrors terminal_env_registry.py: named backends with a default.
type Registry struct {
	backends map[string]Backend
	def      string
}

// NewRegistry seeds local + docker + ssh + singularity + modal + daytona slots.
func NewRegistry(local Backend) *Registry {
	r := &Registry{backends: map[string]Backend{}, def: "local"}
	if local == nil {
		local = &LocalBackend{}
	}
	r.backends["local"] = local
	r.backends["docker"] = NewDocker("botreaper")
	r.backends["ssh"] = NewSSH("", "", "")
	r.backends["singularity"] = NewSingularity("botreaper.sif")
	r.backends["modal"] = NewModal(nil)
	r.backends["daytona"] = NewDaytona(nil)
	return r
}

// Register overrides a backend slot.
func (r *Registry) Register(b Backend) { r.backends[b.Name()] = b }

// Get resolves name (case-insensitive, "" = default).
func (r *Registry) Get(name string) Backend {
	if name == "" {
		name = r.def
	}
	if b, ok := r.backends[strings.ToLower(name)]; ok {
		return b
	}
	return r.backends[r.def]
}

// Names lists slots.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.backends))
	for k := range r.backends {
		out = append(out, k)
	}
	return out
}
