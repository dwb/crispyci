package main

import (
	"bufio"
	"io"
	"os/exec"
	"path"
	"syscall"
	"time"
)

func (self *JobRun) Run(notifyProgress chan JobProgress) (err error) {
	cmd := new(exec.Cmd)
	cmd.Path = path.Join(self.ScriptDir, "run-"+self.Job.ScriptSet)
	setupCmd(cmd)
	cmd.Dir = path.Join(self.WorkingDir, "jobs", self.Job.Name)

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
	self.setStatus(JobStarted)

	go func() {
		stdout := lineChanFromReader(stdoutPipe)
		stderr := lineChanFromReader(stderrPipe)

		processLine := func(line string, ch chan string) {
			if line != "" {
				notifyProgress <- JobProgress{JobRun: *self, Time: time.Now(), Line: line}
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

		self.FinishedAt = time.Now()
		if err == nil {
			self.setStatus(JobSucceeded)
		} else {
			self.setStatus(JobFailed)
		}
	}()

	return nil
}

func (self *JobRun) setStatus(newStatus JobStatus) {
	if newStatus == JobStarted {
		self.StartedAt = time.Now()
	} else {
		self.FinishedAt = time.Now()
	}

	self.Status = newStatus

	if self.statusChanges != nil {
		self.statusChanges <- *self
	}
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
