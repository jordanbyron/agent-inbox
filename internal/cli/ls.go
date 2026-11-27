package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jordanbyron/agent-inbox/internal/snapshot"
	"github.com/jordanbyron/agent-inbox/internal/state"
)

func newLsCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Print the inbox's rows, for other tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !asJSON {
				return errors.New("ls needs --json; it has no other format yet")
			}
			srv, err := findServer(cmd.Context())
			if err != nil {
				return err
			}
			if err := ensureWatcher(cmd.Context(), srv); err != nil {
				return err
			}
			var snap snapshot.Snapshot
			if _, err := state.New(srv.dir).ReadSnapshot(&snap); err != nil {
				return err
			}
			out, err := rowsJSON(snap)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the rows as a JSON array")
	return cmd
}

// rowsJSON is every row in list order, each with the section it is in.
func rowsJSON(snap snapshot.Snapshot) ([]byte, error) {
	rows := []map[string]any{}
	for _, sec := range snap.Sections {
		for _, row := range sec.Rows {
			data, err := json.Marshal(row)
			if err != nil {
				return nil, fmt.Errorf("encoding row %s: %w", row.Key, err)
			}
			fields := map[string]any{}
			if err := json.Unmarshal(data, &fields); err != nil {
				return nil, fmt.Errorf("encoding row %s: %w", row.Key, err)
			}
			fields["section"] = sec.Name
			rows = append(rows, fields)
		}
	}
	return json.MarshalIndent(rows, "", "  ")
}
