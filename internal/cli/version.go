package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/stokaro/ptah-console/internal/buildinfo"
)

func newVersion(stdout io.Writer) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:   "version",
		Short: "Print the build's version and commit",
		Args:  exactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			info := buildinfo.Read()
			switch output {
			case "text":
				modified := ""
				if info.Modified {
					modified = ", modified"
				}
				_, err := fmt.Fprintf(stdout, "ptah-console %s (commit %s%s, %s)\n",
					info.Version, info.Commit, modified, info.GoVersion)
				return err
			case "json":
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(info)
			default:
				return fmt.Errorf("%w: --output %q is not text or json", errUsage, output)
			}
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	return command
}
