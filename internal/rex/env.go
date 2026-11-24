package rex

// Pane is the Rex block a command was run from.
type Pane struct {
	SessionID string
	BlockID   string
}

// PaneFromEnv reads the pane from the variables Rex sets in every block,
// and reports false outside Rex.
func PaneFromEnv(getenv func(string) string) (Pane, bool) {
	pane := Pane{SessionID: getenv("REX_SESSION"), BlockID: getenv("REX_BLOCK")}
	if pane.SessionID == "" || pane.BlockID == "" {
		return Pane{}, false
	}
	return pane, true
}
