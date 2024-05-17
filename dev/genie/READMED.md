# Genie

A tool to operate Dedicated Gitpod installations.

## Architecture
Genie is a single binary that is home to a server and a client. The server is meant to be run within the customers cell, with access to tools like kubectl. The client is run in a Dedicated workspace, for instance.

All interactions happen within a session, which is helpful in multiple ways:
 - reduces the likelihood of clashes in multi-client scenarios (encourages one session per terminal)
 - serves as envelope which the server can enforce timeouts on
 - could be used as hook into substrate (e.g. to get temporary write access to a bucket in the customers cell)

## Protocol
The protocol is a basic request-response that forwards the CLI command, and gets the output of that command back.

Request:
```
sessionID: 2024_05_17_21_55-my-session
id: 7
type: unary
cmd: kubectl
args:
- get
- nodes
context: {}
```

Response:
```
requestID: 7
sequenceID: 0
exitCode: 0
output: |
  NAME                STATUS   ROLES                       AGE    VERSION
  preview-gpl-genie   Ready    control-plane,etcd,master   157m   v1.27.13+k3s1
```

## Example usage

[Get AWS credentials](https://gitpod-eu.awsapps.com/start/#/?tab=accounts) and set them to your terminals

server:
```
go run main.go server run -c server-config.yaml
```

client:
```
go build . && cp genie kubectl
export GENIE_SESSION=$(./genie client session create my-session)
./kubectl get pods
```
