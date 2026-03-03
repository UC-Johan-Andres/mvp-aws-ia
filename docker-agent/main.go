package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
)

const socketPath = "/var/run/docker-agent.sock"

var allowed = map[string]bool{
	"n8n":              true,
	"librechat":        true,
	"chatwoot":         true,
	"chatwoot_sidekiq": true,
	"marimo":           true,
	"bolt":             true,
}

func main() {
	os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Fatalf("chmod socket: %v", err)
	}

	log.Printf("docker-agent listening on %s", socketPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	line := strings.TrimSpace(scanner.Text())
	parts := strings.Fields(line)
	if len(parts) < 2 {
		fmt.Fprintf(conn, "ERROR invalid command\n")
		return
	}

	cmd := parts[0]
	services := parts[1:]

	for _, svc := range services {
		if !allowed[svc] {
			fmt.Fprintf(conn, "ERROR service not allowed\n")
			return
		}
	}

	switch cmd {
	case "STATUS":
		if len(services) != 1 {
			fmt.Fprintf(conn, "ERROR STATUS requires exactly one service\n")
			return
		}
		resp := dockerStatus(services[0])
		fmt.Fprintf(conn, "%s\n", resp)

	case "START":
		args := append([]string{"start"}, services...)
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			fmt.Fprintf(conn, "ERROR %s\n", strings.TrimSpace(string(out)))
			return
		}
		fmt.Fprintf(conn, "OK\n")

	case "STOP":
		args := append([]string{"stop"}, services...)
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			fmt.Fprintf(conn, "ERROR %s\n", strings.TrimSpace(string(out)))
			return
		}
		fmt.Fprintf(conn, "OK\n")

	default:
		fmt.Fprintf(conn, "ERROR unknown command\n")
	}
}

func dockerStatus(service string) string {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", service).Output()
	if err != nil {
		return fmt.Sprintf("ERROR %s", strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "true" {
		return "RUNNING"
	}
	return "STOPPED"
}
