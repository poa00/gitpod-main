// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/client"
	"github.com/gitpod-io/gitpod/genie/pkg/protocol"
)

// kubectlCmd represents the kubectl command
var kubectlCmd = &cobra.Command{
	Use:   "kubectl",
	Short: "forwards all kubectl commands to the current session",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		configPath, _ := cmd.Flags().GetString("config")
		cl, err := client.LoadClient(configPath)
		if err != nil {
			log.WithError(err).Fatal("error creating client")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		sessionId, err := cl.EnsureSession(ctx)
		if err != nil {
			log.WithError(err).WithField("session_id", sessionId).Error("error ensuring session")
		}

		req := protocol.Request{
			SessionID: sessionId,
			Type:      protocol.CallTypeUnary,
			Cmd:       "kubectl",
			Args:      args,
		}
		res, err := cl.Send(ctx, &req)
		if err != nil {
			log.WithError(err).WithField("session_id", sessionId).WithField("request_id", req.ID).Fatal("error sending request")
		}

		fmt.Println(res.Output)
		os.Exit(res.ExitCode)
	},
}

func init() {
	clientCmd.AddCommand(kubectlCmd)
}
