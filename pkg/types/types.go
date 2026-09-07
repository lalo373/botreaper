package types

// Core zero-allocation data types.
// Mirrors Python: run_agent message dicts, model_tools schemas,
// skills/SKILL.md frontmatter, provider profiles, session rows.
// SystemPrompt is immutable per session to preserve LLM KV-cache reuse.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCalls  string `json:"tool_calls,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Tokens     int    `json:"tokens,omitempty"`
	Active     bool   `json:"active"`
}

type ToolParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type Tool struct {
	Name        string      `json:"name"`
	Toolset     string      `json:"toolset"`
	Description string      `json:"description"`
	Params      []ToolParam `json:"params"`
}

type SkillMeta struct {
	Tags          []string `json:"tags"`
	Category      string   `json:"category"`
	RelatedSkills []string `json:"related_skills"`
}

type Skill struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	Author      string    `json:"author"`
	License     string    `json:"license"`
	Platforms   []string  `json:"platforms"`
	Metadata    SkillMeta `json:"metadata"`
	Body        string    `json:"body"`
	CreatedBy   string    `json:"created_by,omitempty"`
}

type Profile struct {
	Name          string `json:"name"`
	BotReaperHome string `json:"botreaper_home"`
}

type Config struct {
	Timezone      string `json:"timezone"`
	MaxIterations int    `json:"max_iterations"`
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url,omitempty"`
	TerminalHome  string `json:"terminal_home_mode,omitempty"`
	MaxResume     int    `json:"max_resume"`
	CJKFTS        bool   `json:"cjk_fts"`
	// Terminal backend selection (written by `botreaper setup terminal`).
	TerminalBackend  string `json:"terminal_backend,omitempty"`
	DockerContainer  string `json:"terminal_docker_container,omitempty"`
	SingularityImage string `json:"terminal_singularity_image,omitempty"`
	SSHHost          string `json:"terminal_ssh_host,omitempty"`
	SSHUser          string `json:"terminal_ssh_user,omitempty"`
	SSHPort          string `json:"terminal_ssh_port,omitempty"`
	SSHKey           string `json:"terminal_ssh_key,omitempty"`
	SearchBase       string `json:"search_base_url,omitempty"`
}
