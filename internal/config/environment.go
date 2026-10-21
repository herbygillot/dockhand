package config

// Variable is an environment variable dockhand reads. Agent is whether
// serve --install keeps it in the agent, as it is when it's run: a path or
// a place serve reads, never a token, since anyone on the Mac can read the
// agent's file.
type Variable struct {
	Name  string
	Agent bool
}

// Environment is every variable dockhand reads, which usage.md's table
// lists, apart from PATH, which serve's agent keeps too, the acceptance
// build's DOCKHAND_FAILPOINT, and tests' DOCKHAND_TEST_* variables.
var Environment = []Variable{
	{Name: "MACPORTS_TREE"},
	{Name: "DOCKHAND_DB"},
	{Name: PathVariable, Agent: true},
	{Name: "GIT_BIN"},
	{Name: "GH_TOKEN"},
	{Name: "GITHUB_TOKEN"},
	{Name: "DOCKHAND_UPSTREAM", Agent: true},
	{Name: "DOCKHAND_PULL_REQUESTS", Agent: true},
	{Name: "DOCKHAND_INDEX_MIRROR", Agent: true},
	{Name: "DOCKHAND_INDEX_CACHE", Agent: true},
	{Name: "DOCKHAND_READING_CACHE", Agent: true},
	{Name: "DOCKHAND_TART_HOME", Agent: true},
	{Name: "TART_HOME", Agent: true},
	{Name: "DOCKHAND_SSH_DIR", Agent: true},
	{Name: "DOCKHAND_GITHUB_CLIENT_ID"},
	{Name: "DOCKHAND_GITHUB_LOG"},
	{Name: "VISUAL"},
	{Name: "EDITOR"},
}

// AgentVariables are the variables serve's agent keeps (Variable.Agent).
func AgentVariables() []string {
	var names []string
	for _, variable := range Environment {
		if variable.Agent {
			names = append(names, variable.Name)
		}
	}
	return names
}
