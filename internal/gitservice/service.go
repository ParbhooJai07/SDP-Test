package gitservice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type Runner struct {
	Binary string
}

type Result struct {
	Stdout string
	Stderr string
}

func NewRunner() Runner {
	return Runner{Binary: "git"}
}

func (r Runner) Run(ctx context.Context, workingDirectory string, args ...string) (Result, error) {
	binary := r.Binary
	if binary == "" {
		binary = "git"
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	if workingDirectory != "" {
		cmd.Dir = workingDirectory
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Result{Stdout: stdout.String(), Stderr: stderr.String()}, fmt.Errorf("git %v failed: %w", args, err)
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String()}, nil
}
