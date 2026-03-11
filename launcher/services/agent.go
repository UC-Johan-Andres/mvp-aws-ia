package services

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"launcher/config"
)

// agentCall sends a single-line command to docker-agent over the unix socket
// and returns the first line of the response.
func agentCall(command string) (string, error) {
	conn, err := net.Dial("unix", config.AgentSocket)
	if err != nil {
		return "", fmt.Errorf("dial agent: %w", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "%s\n", command)
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return "", fmt.Errorf("no response from agent")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// IsRunning queries docker-agent for the STATUS of a service.
func IsRunning(service string) bool {
	resp, err := agentCall("STATUS " + service)
	return err == nil && resp == "RUNNING"
}

// StartContainers sends a START command for the service and any companions.
func StartContainers(service string) error {
	targets := buildTargets(service)
	resp, err := agentCall("START " + strings.Join(targets, " "))
	if err != nil {
		return fmt.Errorf("start %s: %w", service, err)
	}
	if resp != "OK" {
		return fmt.Errorf("start %s: agent replied %q", service, resp)
	}
	return nil
}

// StopContainers sends a STOP command for the service and any companions.
func StopContainers(service string) error {
	targets := buildTargets(service)
	resp, err := agentCall("STOP " + strings.Join(targets, " "))
	if err != nil {
		return fmt.Errorf("stop %s: %w", service, err)
	}
	if resp != "OK" {
		return fmt.Errorf("stop %s: agent replied %q", service, resp)
	}
	return nil
}

// buildTargets returns the service plus any configured companion containers.
func buildTargets(service string) []string {
	targets := []string{service}
	if extra, ok := config.Companions[service]; ok {
		targets = append(targets, extra...)
	}
	return targets
}
