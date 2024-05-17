// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/server"
)

var serverRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a genie server",
	Args:  cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := server.LoadConfig(configPath)
		if err != nil {
			log.WithError(err).Fatal("cannot load config")
		}
		server := server.NewGenieServer(cfg)

		ctx, cancel := context.WithCancel(context.Background())
		cancel = sync.OnceFunc(cancel)
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		go func() {
			<-sigChan
			log.Println("received SIGTERM, quitting.")
			cancel()
		}()

		err = server.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.WithError(err).Fatal("error running server")
		}
	},
}

func init() {
	serverCmd.AddCommand(serverRunCmd)
}
