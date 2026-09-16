package buildverify

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Runner struct {
	DataDirectory string
	Timeout       time.Duration
}

func (r Runner) Run(ctx context.Context, projectPath, projectID, taskID string, action Action, report LineReporter) (Run, error) {
	started := time.Now().UTC()
	run := Run{ID: fmt.Sprintf("build-%d", started.UnixNano()), ProjectID: projectID, TaskID: taskID, ActionID: action.ID, ActionLabel: action.Label, Command: commandDisplay(action), Status: "running", ExitCode: -1, StartedAt: started}
	root, err := filepath.Abs(projectPath)
	if err != nil {
		return run, err
	}
	cwd := filepath.Clean(filepath.Join(root, filepath.FromSlash(action.WorkingDir)))
	relative, err := filepath.Rel(root, cwd)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return run, fmt.Errorf("build working directory escapes the project")
	}
	if r.Timeout <= 0 {
		r.Timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	logDirectory := filepath.Join(r.DataDirectory, "build-logs", projectID)
	if err := os.MkdirAll(logDirectory, 0o700); err != nil {
		return run, fmt.Errorf("create build log directory: %w", err)
	}
	run.LogPath = filepath.Join(logDirectory, run.ID+".log")
	logFile, err := os.OpenFile(run.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return run, fmt.Errorf("create build log: %w", err)
	}
	defer logFile.Close()

	executable := action.Executable
	args := append([]string(nil), action.Arguments...)
	if runtime.GOOS == "windows" && strings.EqualFold(executable, "npm") {
		executable = "npm.cmd"
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = cwd
	configureHiddenProcess(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return run, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return run, err
	}
	if report != nil {
		report("info", "$ "+run.Command)
	}
	if _, err := fmt.Fprintf(logFile, "[%s] cwd=%s\n$ %s\n", started.Format(time.RFC3339), cwd, run.Command); err != nil {
		return run, err
	}
	if err := command.Start(); err != nil {
		return finishRun(run, started, -1, "Could not start build command: "+err.Error()), err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go streamOutput(stdout, logFile, "info", report, &wg)
	go streamOutput(stderr, logFile, "error", report, &wg)
	waitErr := command.Wait()
	wg.Wait()
	exitCode := command.ProcessState.ExitCode()
	if ctx.Err() == context.DeadlineExceeded {
		run = finishRun(run, started, exitCode, fmt.Sprintf("Build timed out after %s.", r.Timeout))
		run.Status = "timeout"
		return run, ctx.Err()
	}
	if waitErr != nil {
		run = finishRun(run, started, exitCode, fmt.Sprintf("%s failed with exit code %d.", action.Label, exitCode))
		return run, waitErr
	}
	run = finishRun(run, started, exitCode, action.Label+" completed successfully.")
	run.Status = "passed"
	return run, nil
}

func finishRun(run Run, started time.Time, exitCode int, reason string) Run {
	now := time.Now().UTC()
	run.CompletedAt = &now
	run.DurationMS = time.Since(started).Milliseconds()
	run.ExitCode = exitCode
	run.Reason = reason
	if run.Status == "running" {
		run.Status = "failed"
	}
	return run
}
func streamOutput(reader io.Reader, writer io.Writer, level string, report LineReporter, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(writer, line)
		if report != nil {
			report(level, line)
		}
	}
}
func commandDisplay(action Action) string {
	values := append([]string{action.Executable}, action.Arguments...)
	return strings.Join(values, " ")
}
