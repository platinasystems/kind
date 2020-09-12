/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or impliep.
See the License for the specific language governing permissions and
limitations under the License.
*/

package docker

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
)

// nodes.Node implementation for the docker provider
type node struct {
	name string
}

func (n *node) String() string {
	return n.name
}

func (n *node) Role() (string, error) {
	cmd := exec.Command("docker", "inspect",
		"--format", fmt.Sprintf(`{{ index .Config.Labels "%s"}}`, nodeRoleLabelKey),
		n.name,
	)
	lines, err := exec.OutputLines(cmd)
	if err != nil {
		return "", errors.Wrap(err, "failed to get role for node")
	}
	if len(lines) != 1 {
		return "", errors.Errorf("failed to get role for node: output lines %d != 1", len(lines))
	}
	return lines[0], nil
}

func (n *node) IP(iface string) (ipv4 string, ipv6 string, err error) {
	// retrieve the IP address of the node using docker inspect
	// cmd := exec.Command("docker", "inspect",
	//	"-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}},{{.GlobalIPv6Address}}{{end}}",
	//	n.name, // ... against the "node" container
	//)
	//lines, err := exec.OutputLines(cmd)
	//if err != nil {
	//	return "", "", errors.Wrap(err, "failed to get container details")
	//}
	//if len(lines) != 1 {
	//	return "", "", errors.Errorf("file should only be one line, got %d lines", len(lines))
	//}
	//ips := strings.Split(lines[0], ",")
	//if len(ips) != 2 {
	//	return "", "", errors.Errorf("container addresses should have 2 values, got %d values", len(ips))
	//}
	//return ips[0], ips[1], nil

	cmd := n.Command("ip", "addr", "show", iface)
	lines, err := exec.CombinedOutputLines(cmd)

	for i := 0; i < len(lines); i++ {
		if strings.Contains(lines[i], "inet ") {
			re := regexp.MustCompile(`inet (.*?) brd`)
			if ipv4 == "" {
				ipv4 = strings.Split(re.FindStringSubmatch(lines[i])[1], "/")[0]
				// fmt.Printf("\ni %d, len %d, ipv4 %s", i, len(ipv4), ipv4)
			}
		}
		if strings.Contains(lines[i], "inet6") {
			re := regexp.MustCompile(`inet6 (.*?) scope`)
			if ipv6 == "" {
				ipv6 = strings.Split(re.FindStringSubmatch(lines[i])[1], "/")[0]
				// fmt.Printf("\ni %d, len %d, ipv6 %s", i, len(ipv6), ipv6)
			}
		}
	}

	if (ipv4 == "" && ipv6 == "") {
		return "", "", errors.Errorf("container ipv4 & ipv6 addresses NOT found")
	}
	if (err == nil) {
		err = n.AddEtcHostsEntry(ipv4)
	}
	return ipv4, ipv6, nil
}

func (n *node) AddEtcHostsEntry(ipv4 string) (err error) {

	he := fmt.Sprintf("echo \"%s    %s\" >> /etc/hosts", ipv4, n.String())
	err = n.Command("sh", "-c", he).Run()

	return
}

func (n *node) Command(command string, args ...string) exec.Cmd {
	return &nodeCmd{
		nameOrID: n.name,
		command:  command,
		args:     args,
	}
}

func (n *node) CommandContext(ctx context.Context, command string, args ...string) exec.Cmd {
	return &nodeCmd{
		nameOrID: n.name,
		command:  command,
		args:     args,
		ctx:      ctx,
	}
}

// nodeCmd implements exec.Cmd for docker nodes
type nodeCmd struct {
	nameOrID string // the container name or ID
	command  string
	args     []string
	env      []string
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	ctx      context.Context
}

func (c *nodeCmd) String() string {
	return c.command + " " + strings.Join(c.args, " ")
}

func (c *nodeCmd) Run() error {
	args := []string{
		"exec",
		// run with privileges so we can remount etc..
		// this might not make sense in the most general sense, but it is
		// important to many kind commands
		"--privileged",
	}
	if c.stdin != nil {
		args = append(args,
			"-i", // interactive so we can supply input
		)
	}
	// set env
	for _, env := range c.env {
		args = append(args, "-e", env)
	}
	// specify the container and command, after this everything will be
	// args the command in the container rather than to docker
	args = append(
		args,
		c.nameOrID, // ... against the container
		c.command,  // with the command specified
	)
	args = append(
		args,
		// finally, with the caller args
		c.args...,
	)
	var cmd exec.Cmd
	if c.ctx != nil {
		cmd = exec.CommandContext(c.ctx, "docker", args...)
	} else {
		cmd = exec.Command("docker", args...)
	}
	if c.stdin != nil {
		cmd.SetStdin(c.stdin)
	}
	if c.stderr != nil {
		cmd.SetStderr(c.stderr)
	}
	if c.stdout != nil {
		cmd.SetStdout(c.stdout)
	}
	return cmd.Run()
}

func (c *nodeCmd) SetEnv(env ...string) exec.Cmd {
	c.env = env
	return c
}

func (c *nodeCmd) SetStdin(r io.Reader) exec.Cmd {
	c.stdin = r
	return c
}

func (c *nodeCmd) SetStdout(w io.Writer) exec.Cmd {
	c.stdout = w
	return c
}

func (c *nodeCmd) SetStderr(w io.Writer) exec.Cmd {
	c.stderr = w
	return c
}

func (n *node) SerialLogs(w io.Writer) error {
	return exec.Command("docker", "logs", n.name).SetStdout(w).SetStderr(w).Run()
}
