// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/server"
)

var serverRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a genie server",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		configFile := args[0]

		cfg, err := server.LoadConfig(configFile)
		if err != nil {
			log.WithError(err).Fatal("cannot load config")
		}
		server := server.NewGenieServer(cfg)

		err = server.Run()
		if err != nil {
			log.WithError(err).Fatal("error running server")
		}
	},
}

func init() {
	serverCmd.AddCommand(serverRunCmd)
}
