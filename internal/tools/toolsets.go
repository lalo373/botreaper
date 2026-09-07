package tools

import "sort"

// Toolsets mirrors toolsets.py TOOLSETS + _HERMES_CORE_TOOLS. Every model
// tool ships on every call, so membership here is the cost lever.
var coreTools = []string{
	"read_file", "write_file", "patch", "search_files",
	"terminal", "web_search", "web_extract", "todo",
	"memory", "session_search", "skills", "delegate_task",
}

// Toolsets maps toolset name -> member tool names.
var Toolsets = map[string][]string{
	"file":           {"read_file", "write_file", "patch", "search_files"},
	"terminal":       {"terminal"},
	"web":            {"web_search", "web_extract"},
	"search":         {"web_search"},
	"todo":           {"todo"},
	"memory":         {"memory"},
	"session_search": {"session_search"},
	"skills":         {"skills"},
	"delegation":     {"delegate_task"},
	"cronjob":        {"cron_create", "cron_list", "cron_delete"},
	"clarify":        {"clarify"},
	"project":        {"project_info"},
	"coding":         {"read_file", "write_file", "patch", "search_files", "terminal"},
	"safe":           {"read_file", "search_files", "web_search"},
}

// ResolveToolset expands one name to its members.
func ResolveToolset(name string) []string {
	if m, ok := Toolsets[name]; ok {
		cp := append([]string{}, m...)
		sort.Strings(cp)
		return cp
	}
	return []string{name}
}

// ResolveMultiple unions several toolsets.
func ResolveMultiple(names []string) []string {
	seen := map[string]bool{}
	for _, n := range names {
		for _, m := range ResolveToolset(n) {
			seen[m] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CoreTools returns the narrow-waist default set.
func CoreTools() []string { return append([]string{}, coreTools...) }

// ToolsetNames lists known toolsets.
func ToolsetNames() []string {
	out := make([]string, 0, len(Toolsets))
	for k := range Toolsets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
