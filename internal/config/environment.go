package config

// Variable is an environment variable dockhand reads. Agent is whether
// serve --install keeps it in the agent, as it is when it's run: a path or
// a place serve reads, never a token, since anyone on the Mac can read the
// agent's file.
type Variable struct {
	Name  string
	Agent bool
}

// Environment is every variable dockhand reads, which docs/usage/agents.md's table
// lists, apart from HOME and XDG_CACHE_HOME; PATH, which serve's agent keeps too; the
// acceptance build's DOCKHAND_FAILPOINT; and tests' DOCKHAND_TEST_*
// variables.
var Environment = []Variable{
	{Name: "MACPORTS_TREE"},
	{Name: "DOCKHAND_DB"},
	{Name: PathVariable, Agent: true},
	// The programs dockhand runs, each named by its variable after --tool
	// and before [tools] (Programs); serve's agent carries the ones named
	// as --tool, resolved where it's installed.
	{Name: "GIT_BIN"},
	{Name: "PORT_TCLSH_BIN"},
	{Name: "TART_BIN"},
	{Name: "GO2PORT_BIN"},
	{Name: "CARGO2PORT_BIN"},
	{Name: "XCODES_BIN"},
	{Name: "GH_BIN"},
	{Name: "PATCH_BIN"},
	{Name: "DIFF_BIN"},
	// A token in GH_TOKEN, else GITHUB_TOKEN, overrides dockhand's login,
	// which the GitHub CLI's comes after (compose.Options.Signin).
	{Name: "GH_TOKEN"},
	{Name: "GITHUB_TOKEN"},
	{Name: "DOCKHAND_UPSTREAM", Agent: true},
	{Name: "DOCKHAND_PULL_REQUESTS", Agent: true},
	{Name: "DOCKHAND_INDEX_MIRROR", Agent: true},
	{Name: "DOCKHAND_INDEX_CACHE", Agent: true},
	{Name: "DOCKHAND_READING_CACHE", Agent: true},
	// Where GitHub's answers are kept with their ETags (github.
	// ConditionalCache).
	{Name: "DOCKHAND_GITHUB_CACHE", Agent: true},
	// Where packages.macports.org's listings are kept
	// (binaryarchive.Catalog), and ports.macports.org's build histories
	// (buildhistory.Reader).
	{Name: "DOCKHAND_ARCHIVE_CACHE", Agent: true},
	{Name: "DOCKHAND_BUILD_HISTORY_CACHE", Agent: true},
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
