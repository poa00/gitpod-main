// Copyright (c) 2024 Gitpod GmbH. All rights reserved.
// Licensed under the GNU Affero General Public License (AGPL).
// See License.AGPL.txt in the project root for license information.

package protocol

type Request struct {
	SessionID string `yaml:"sessionID"`

	// ID is the unique identifier of the request within the session
	ID int `yaml:"id"`

	Type CallType `yaml:"type"`

	// Cmd is the command to execute
	Cmd string `yaml:"cmd"`

	// Args are the arguments to the command
	Args []string `yaml:"args"`

	// Context is the context in which the request is executed
	Context Context `yaml:"context"`
}

type CallType string

const (
	CallTypeUnary  CallType = "unary"
	CallTypeStream CallType = "stream"
)

type Context struct {
	// Timeout is the maximum time the request is allowed to take in milliseconds
	Timeout int `yaml:"timeout,omitempty"`
}

type Response struct {
	// RequestID is the unique identifier of the request
	RequestID int `yaml:"requestID"`

	// SequenceID is the sequence number of the response (if the response is part of a stream)
	SequenceID int `yaml:"sequenceID"`

	// ExitCode is the rc of the command
	ExitCode int `yaml:"exitCode"`

	// Output is the output of the command
	Output string `yaml:"output"`
}
