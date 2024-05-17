// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/client"
)

// sessionCreateCmd represents the describe command
var sessionCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "creates a new session with that name",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if name == "" {
			log.Fatal("session name is required but not provided")
		}

		configPath, _ := cmd.Flags().GetString("config")
		cl, err := client.LoadClient(configPath)
		if err != nil {
			log.WithError(err).Fatal("error creating client")
		}

		ctx := context.Background()
		sessionId, err := cl.CreateSession(ctx, name)
		if err != nil {
			log.WithError(err).WithField("session_name", name).Fatal("error creating session")
		}

		cl.Config.CurrentSession = sessionId
		err = client.StoreConfig(configPath, cl.Config)
		if err != nil {
			log.WithError(err).WithField("session_id", sessionId).Fatal("updating current session")
		}
		log.WithField("session_id", sessionId).Info("session created and set as current session")
	},
}

func init() {
	clientSessionCmd.AddCommand(sessionCreateCmd)
}
