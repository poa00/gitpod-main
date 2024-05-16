// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package server

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v2"

	"github.com/gitpod-io/gitpod/common-go/log"
	"github.com/gitpod-io/gitpod/genie/pkg/transport"
)

type Config struct {
	Transport transport.TransportConfig `yaml:"transport"`
}

type GenieServer struct {
	Config *Config
}

func NewGenieServer(cfg *Config) *GenieServer {
	return &GenieServer{
		Config: cfg,
	}
}

func (g *GenieServer) Run() error {
	// Do nothing while waiting for SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("received SIGTERM, quitting.")

	return nil
}

func LoadConfig(path string) (*Config, error) {
	yamlFile, err := os.ReadFile("conf.yaml")
	if err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	c := &Config{}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		return nil, fmt.Errorf("Error parsing config file: %v", err)
	}

	return c, nil
}
