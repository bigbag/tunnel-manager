package forward

import (
	"fmt"

	"github.com/bigbag/tunnel-manager/internal/config"
)

func ListenAddr(port int) string {
	return fmt.Sprintf(":%d", port)
}

func KubectlArgs(t *config.Tunnel) []string {
	args := []string{
		"kubectl",
		"port-forward",
		"--namespace=" + t.Namespace,
		"--context=" + t.Context,
	}
	if t.Address != "" {
		args = append(args, "--address="+t.Address)
	}
	args = append(args,
		"service/"+t.Service,
		fmt.Sprintf("%d:%d", t.LocalPort, t.RemotePort),
	)
	return args
}

type AuthChoice struct {
	KeyPath     string
	UseAgent    bool
	UsePassword bool
	Logs        []string
}

func ChooseAuth(identityFile, password string, agentAvailable, keyExists bool) AuthChoice {
	var choice AuthChoice
	if identityFile != "" {
		if keyExists {
			choice.KeyPath = identityFile
			choice.Logs = append(choice.Logs, "Using key: "+identityFile)
		} else {
			choice.Logs = append(choice.Logs, "Key not found: "+identityFile)
		}
	}
	choice.UseAgent = agentAvailable
	choice.UsePassword = password != ""
	if choice.UsePassword {
		choice.Logs = append(choice.Logs, "Using password authentication")
	} else {
		choice.Logs = append(choice.Logs, "Using key/agent authentication")
	}
	return choice
}
