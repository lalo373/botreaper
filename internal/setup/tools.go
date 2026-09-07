package setup

// SetupTools configures API keys for the tool providers this runtime
// implements. (Python delegates to the full `hermes tools` selector; the Go
// tool surface needing keys is web search.)
func SetupTools(s *State) {
	Header("Tools", false)
	emit("Configure API keys for tool providers.")
	emit("")

	cur := s.Get("search_base_url", "")
	q := "Web search base URL (empty to disable)"
	if cur != "" {
		q += " [" + cur + "]"
	}
	v := Prompt(q, "")
	if v != "" {
		s.Set("search_base_url", v)
		key := PromptSecret("Web search API key (or Enter to skip)")
		if key != "" {
			if err := s.SetSecret("SEARCH_API_KEY", key); err != nil {
				Error("Could not save key: %v", err)
				return
			}
			emit("Web search key saved.")
		}
	} else if cur != "" {
		s.Set("search_base_url", "")
		_ = s.ClearSecret("SEARCH_API_KEY")
		emit("Web search disabled.")
	}
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("Tools configured.")
}
