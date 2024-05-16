// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package cmd

import (
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"

	"github.com/gitpod-io/gitpod/genie/pkg/util"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run a genie server",
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverCmd.PersistentFlags().StringP("kubeconfig", "", "", "Path to the kubeconfig file")
}

func getKubeconfig() (*rest.Config, string, error) {
	kubeconfig, err := rootCmd.PersistentFlags().GetString("kubeconfig")
	if err != nil {
		return nil, "", err
	}

	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	if kubeconfig == "" {
		home, err := homedir.Dir()
		if err != nil {
			return nil, "", err
		}
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	return util.GetKubeconfig(kubeconfig)
}
