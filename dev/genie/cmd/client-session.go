// Copyright (c) 2020 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"github.com/spf13/cobra"
)

// clientSessionCmd represents the "client session" command
var clientSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Allows to manage sessions",
}

func init() {
	clientCmd.AddCommand(clientSessionCmd)
}
