package main

import (
	"bufio"
	"io"
	"os/exec"
	"syscall"
	"time"
)

func (self *Job) Run(notifyProgress chan JobProgress, notifyFinish chan JobResult) (err error) {
	cmd := exec.Command("./run-" + self.ScriptSet)
	setupCmd(cmd)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return
	}

	err = cmd.Start()
	if err != nil {
		return
	}
	jobResult := JobResult{Job: *self, StartedAt: time.Now()}

	go func() {
		stdout := lineChanFromReader(stdoutPipe)
		stderr := lineChanFromReader(stderrPipe)
		output := ""

		processLine := func(line string, ch chan string) {
			if line != "" {
				notifyProgress <- JobProgress{Job: *self, Line: line}
				output += line
			}
		}

		for {
			select {
			case line, ok := <-stdout:
				processLine(line, stdout)
				if !ok {
					stdout = nil
				}
			case line, ok := <-stderr:
				processLine(line, stderr)
				if !ok {
					stderr = nil
				}
			}

			if stdout == nil && stderr == nil {
				break
			}
		}

		err = cmd.Wait()

		jobResult.FinishedAt = time.Now()
		jobResult.Output = output
		if err == nil {
			jobResult.Status = JobSucceeded
		} else {
			jobResult.Status = JobFailed
		}

		notifyFinish <- jobResult
	}()

	return nil
}

func lineChanFromReader(reader io.Reader) (chanOut chan string) {
	buffered := bufio.NewReader(reader)
	chanOut = make(chan string)
	go func() {
		for {
			line, err := buffered.ReadString('\n')
			if line != "" {
				chanOut <- line
			}
			if err != nil {
				break
			}
		}
		close(chanOut)
	}()
	return
}

func setupCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	/* If we put the command in a new process group, a SIGINT/Ctrl-C won't get
	 * passed through to it.
	 */
	cmd.SysProcAttr.Setpgid = true
}
