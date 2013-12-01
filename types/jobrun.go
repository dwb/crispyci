package types

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path"
	"syscall"
	"time"
)

func (self *JobRun) Run(notifyProgress chan JobProgress) (err error) {
	cmd := new(exec.Cmd)
	cmd.Path = path.Join(self.ScriptDir, "run-"+self.Job.ScriptSet)

	if cmd.Path[0] != '/' {
		// Make absolute path before continuing, as setting Dir will break
		// relative paths.
		pwd, err := os.Getwd()
		if err != nil {
			return err
		}
		cmd.Path = path.Join(pwd, cmd.Path)
	}

	cmd.Dir = path.Join(self.WorkingDir, "jobs", self.Job.Name)
	setupCmd(cmd)

	if _, err = os.Stat(cmd.Dir); os.IsNotExist(err) {
		err = os.MkdirAll(cmd.Dir, 0750|os.ModeDir)
		if err != nil {
			return
		}
	}

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

		var killChan <-chan time.Time
		aborted := false

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

			case <-self.interruptChan:
				aborted = true
				self.interruptChan = nil
				cmd.Process.Signal(syscall.SIGTERM)
				killChan = time.After(5 * time.Second)

			case <-killChan:
				cmd.Process.Signal(os.Kill)
			}

			if stdout == nil && stderr == nil {
				break
			}
		}

		self.interruptChan = nil
		notifyProgress <- JobProgress{JobRun: *self, Time: time.Now(),
			IsFinal: true}

		err = cmd.Wait()

		self.FinishedAt = time.Now()

		if aborted {
			self.setStatus(JobAborted)
		} else if err == nil {
			self.setStatus(JobSucceeded)
		} else {
			self.setStatus(JobFailed)
		}
	}()

	return nil
}

func (self *JobRun) Abort() {
	select {
	case self.interruptChan <- true:
	default:
	}
}

// --- Private ---

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
