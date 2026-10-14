package hostctl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const maxCommandOutputBytes = 1 << 20

type Command struct {
	Name string
	Args []string
	Env  map[string]string
}

type CommandResult struct {
	Output string
}

type CommandRunner interface {
	Run(context.Context, Command) (CommandResult, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if command.Name == "" || strings.ContainsAny(command.Name, "\x00\r\n") {
		return CommandResult{}, errors.New("invalid command name")
	}
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	environment, err := buildCommandEnvironment(command, os.Environ())
	if err != nil {
		return CommandResult{}, err
	}
	process.Env = environment
	var output boundedBuffer
	process.Stdout = &output
	process.Stderr = &output
	if err := process.Run(); err != nil {
		return CommandResult{Output: output.String()}, fmt.Errorf("command %s failed", filepathBase(command.Name))
	}
	if output.truncated {
		return CommandResult{}, fmt.Errorf("command %s output exceeded limit", filepathBase(command.Name))
	}
	return CommandResult{Output: output.String()}, nil
}

func buildCommandEnvironment(command Command, inherited []string) ([]string, error) {
	overrides := make(map[string]string, len(command.Env))
	for key, value := range command.Env {
		if key == "" || strings.ContainsAny(key, "=\x00\r\n") || strings.ContainsAny(value, "\x00") {
			return nil, errors.New("invalid command environment")
		}
		overrides[key] = value
	}
	postgresClient := false
	switch filepathBase(command.Name) {
	case "pg_dump", "pg_restore", "createdb":
		postgresClient = true
	}
	environment := make([]string, 0, len(inherited)+len(overrides))
	for _, entry := range inherited {
		key, _, present := strings.Cut(entry, "=")
		if !present || key == "" {
			continue
		}
		if _, replaced := overrides[key]; replaced || (postgresClient && strings.HasPrefix(key, "PG")) {
			continue
		}
		environment = append(environment, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment, nil
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	truncated bool
}

func (writer *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := maxCommandOutputBytes - writer.buffer.Len()
	if remaining <= 0 {
		writer.truncated = true
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		writer.truncated = true
	}
	_, _ = writer.buffer.Write(value)
	return original, nil
}

func (writer *boundedBuffer) String() string {
	return writer.buffer.String()
}

func filepathBase(value string) string {
	value = strings.TrimRight(value, "/")
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		return value[index+1:]
	}
	return value
}
