package setup

import "strconv"

// SetupAgentSettings is the Agent Settings section: max iterations with the
// verbatim Python prompt. (Compression, tool-progress, and reset-policy
// screens have no Go runtime knobs yet and are omitted.)
func SetupAgentSettings(s *State) {
	Header("Agent Settings", false)
	emit("   Guide: https://hermes-agent.nousresearch.com/docs/user-guide/configuration")
	emit("")
	emit("Maximum tool-calling iterations per conversation.")
	emit("Higher = more complex tasks, but costs more tokens.")
	cur := s.Get("max_iterations", "500")
	emit("Press Enter to keep %s. Use 90 for most tasks or 150+ for open exploration.", cur)
	v := Prompt("Max iterations ["+cur+"]", "")
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		emit("Invalid number, keeping current value")
		return
	}
	s.Set("max_iterations", strconv.Itoa(n))
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("Max iterations set to %d", n)
}
