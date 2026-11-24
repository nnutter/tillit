package daemon

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nnutter/tillit/internal/daemonlog"
	"github.com/nnutter/tillit/internal/repository"
	"github.com/nnutter/tillit/internal/subprocess"
)

const (
	lifecycleTimeout = 10 * time.Second
	gracefulTimeout  = 2 * time.Second
	maxSocketPath    = 103
)

func lifecyclePaths(repoPath string) (string, string, string, error) {
	repo, gitDir, err := repository.Open(repoPath)
	if err != nil {
		return "", "", "", err
	}
	_ = repo.Close()
	gitDir, err = filepath.Abs(gitDir)
	if err != nil {
		return "", "", "", err
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", "", "", err
	}
	socket := filepath.Join(gitDir, "tillit", "daemon.sock")
	if len(socket) > maxSocketPath {
		return "", "", "", fmt.Errorf("daemon socket path is too long: %s", socket)
	}
	return root, gitDir, socket, nil
}

// Start launches a detached, supervised daemon and waits for readiness.
func Start(ctx context.Context, repoPath string) (string, error) {
	root, gitDir, _, err := lifecyclePaths(repoPath)
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer func() { _ = read.Close() }()
	cmd := exec.Command(exe, "__tillit_supervisor", root)
	cmd.Stdin = zeroReader()
	cmd.Stdout, cmd.Stderr = zeroReader(), zeroReader()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.ExtraFiles = []*os.File{write}
	if err := cmd.Start(); err != nil {
		_ = write.Close()
		return "", fmt.Errorf("start daemon supervisor: %w", err)
	}
	_ = write.Close()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	result := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(read).ReadString('\n')
		result <- strings.TrimSpace(line)
	}()
	if err := waitForReady(ctx, result, cmd, waited); err != nil {
		return "", err
	}
	return filepath.Join(gitDir, "tillit", "daemon.log"), nil
}

func waitForReady(ctx context.Context, result <-chan string, cmd *exec.Cmd, waited <-chan error) error {
	timer := time.NewTimer(lifecycleTimeout)
	defer timer.Stop()
	select {
	case message := <-result:
		return acceptReady(message, cmd, waited)
	case <-ctx.Done():
		stopOwned(cmd, waited)
		return ctx.Err()
	case <-timer.C:
		stopOwned(cmd, waited)
		return errors.New("daemon startup timed out")
	}
}

func acceptReady(message string, cmd *exec.Cmd, waited <-chan error) error {
	if message == "ready" {
		return nil
	}
	stopOwned(cmd, waited)
	if message == "" {
		message = "supervisor exited before readiness"
	}
	return fmt.Errorf("start daemon: %s", message)
}

func zeroReader() *os.File {
	file, err := os.Open(os.DevNull)
	if err != nil {
		return nil
	}
	return file
}

func stopOwned(cmd *exec.Cmd, waited <-chan error) {
	signalOwned(cmd.Process, syscall.SIGTERM)
	select {
	case err := <-waited:
		ignoreExit(err)
	case <-time.After(time.Second):
		signalOwned(cmd.Process, syscall.SIGKILL)
		ignoreExit(<-waited)
	}
}

func ignoreExit(err error) {
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return
	}
}

func signalOwned(process *os.Process, signal syscall.Signal) {
	if process == nil {
		return
	}
	err := process.Signal(signal)
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return
	}
}

// Stop asks the live supervisor to stop its owned daemon.
func Stop(ctx context.Context, repoPath string, kill bool) error {
	_, _, socket, err := lifecyclePaths(repoPath)
	if err != nil {
		return err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return stopOrphan(repoPath, kill)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(lifecycleTimeout)); err != nil {
		return err
	}
	request := "stop\n"
	if kill {
		request = "kill\n"
	}
	if _, err := io.WriteString(conn, request); err != nil {
		return fmt.Errorf("request daemon stop: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("daemon did not stop; retry with tillit stop --kill: %w", err)
	}
	if message := strings.TrimSpace(line); message != "stopped" {
		return fmt.Errorf("stop daemon: %s", message)
	}
	return nil
}

func stopOrphan(repoPath string, kill bool) error {
	root, gitDir, _, err := lifecyclePaths(repoPath)
	if err != nil {
		return err
	}
	if !workerVerified(root, gitDir) {
		return errors.New("no supervised daemon available")
	}
	if !kill {
		return errors.New("daemon supervisor is unavailable; retry with tillit stop --kill")
	}
	pid, ok := verifiedWorker(root, gitDir)
	if !ok {
		return errors.New("refusing to signal an unrelated process")
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	deadline := time.Now().Add(lifecycleTimeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("daemon did not exit after forced stop")
}

func verifiedWorker(root, gitDir string) (int, bool) {
	lockPath, err := worktreeLockPath(gitDir)
	if err != nil {
		return 0, false
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || invalidPID(pid) {
		return 0, false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return 0, false
	}
	command := string(out)
	if strings.Contains(command, "__tillit_worker") && strings.Contains(command, root) {
		return pid, true
	}
	return 0, false
}

func workerVerified(root, gitDir string) bool {
	_, ok := verifiedWorker(root, gitDir)
	return ok
}

func invalidPID(pid int) bool {
	return pid <= 0
}

type stopRequest struct {
	kill   bool
	result chan string
	done   chan struct{}
}

func (r stopRequest) respond(message string) {
	if r.result == nil {
		return
	}
	select {
	case r.result <- message:
		<-r.done
	default:
	}
}

// Supervise owns the worker process and provides responsive stop control.
func Supervise(repoPath string, startup *os.File) error {
	defer func() { _ = startup.Close() }()
	return supervise(repoPath, startup, nil)
}

// Foreground runs the same supervised daemon attached to the caller.
func Foreground(repoPath string, output io.Writer) error {
	return supervise(repoPath, io.Discard, output)
}

func supervise(repoPath string, startup, output io.Writer) (result error) {
	sent := false
	defer func() {
		if !sent {
			message := "supervisor exited before readiness"
			if result != nil {
				message = result.Error()
			}
			_, _ = fmt.Fprintln(startup, message)
		}
	}()
	root, gitDir, socket, err := lifecyclePaths(repoPath)
	if err != nil {
		return err
	}
	lock, err := lockSupervisor(gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	listener, err := listenControl(socket)
	if err != nil {
		return err
	}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			_ = listener.Close()
			_ = os.Remove(socket)
			_ = lock.Close()
		})
	}
	defer cleanup()
	log, err := daemonlog.Open(gitDir)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	var logs io.Writer = log
	if output != nil {
		logs = io.MultiWriter(log, output)
	}
	return superviseWorker(root, startup, listener, logs, &sent, cleanup)
}

func lockSupervisor(gitDir string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Join(gitDir, "tillit"), 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(gitDir, "tillit", "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		if supervisorBusy(err) {
			return nil, errors.New("daemon already running")
		}
		return nil, err
	}
	return lock, nil
}

func supervisorBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func listenControl(socket string) (net.Listener, error) {
	_ = os.Remove(socket)
	oldMask := syscall.Umask(0o177)
	listener, err := net.Listen("unix", socket)
	syscall.Umask(oldMask)
	if err != nil {
		return nil, err
	}
	return listener, nil
}

func superviseWorker(root string, startup io.Writer, listener net.Listener, log io.Writer, sent *bool, cleanup func()) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = readyRead.Close(); _ = readyWrite.Close() }()
	liveRead, liveWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = liveRead.Close(); _ = liveWrite.Close() }()
	parentRead, parentWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = parentRead.Close(); _ = parentWrite.Close() }()
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = outputRead.Close(); _ = outputWrite.Close() }()
	cmd := exec.Command(exe, "__tillit_worker", root)
	cmd.Stdin = zeroReader()
	cmd.Stdout, cmd.Stderr = outputWrite, outputWrite
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{readyWrite, liveWrite, parentRead}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = readyWrite.Close()
	_ = liveWrite.Close()
	_ = parentRead.Close()
	_ = outputWrite.Close()
	outputDone := make(chan struct{})
	go func() { _, _ = io.Copy(log, outputRead); close(outputDone) }()
	workerReady := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(readyRead).ReadString('\n')
		workerReady <- strings.TrimSpace(line)
	}()
	workerDead := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, liveRead); close(workerDead) }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	requests := make(chan stopRequest)
	go acceptStops(listener, requests)

	session := &supervisorSession{
		cmd: cmd, startup: startup, sent: sent, cleanup: cleanup,
		outputDone: outputDone, workerReady: workerReady, workerDead: workerDead,
		signals: signals, requests: requests, readyC: workerReady,
	}
	defer session.finish()
	return session.run()
}

type supervisorSession struct {
	cmd           *exec.Cmd
	startup       io.Writer
	sent          *bool
	cleanup       func()
	outputDone    <-chan struct{}
	workerReady   <-chan string
	workerDead    <-chan struct{}
	signals       <-chan os.Signal
	requests      <-chan stopRequest
	readyC        <-chan string
	reaped        bool
	readyReported bool
	graceful      *time.Timer
	gracefulReq   *stopRequest
	killWaiters   []stopRequest
}

func (s *supervisorSession) expireGraceful() {
	if s.gracefulReq != nil {
		if s.gracefulReq.result == nil {
			s.killGroup(syscall.SIGKILL)
		} else {
			s.gracefulReq.respond("daemon did not stop; retry with tillit stop --kill")
		}
	}
	s.gracefulReq = nil
	s.graceful = nil
}

func (s *supervisorSession) failStartup(message string) {
	_, _ = fmt.Fprintln(s.startup, message)
	*s.sent = true
}

func (s *supervisorSession) finish() {
	if s.reaped {
		return
	}
	s.killGroup(syscall.SIGKILL)
	ignoreExit(s.cmd.Wait())
	<-s.outputDone
	s.cleanup()
	s.reaped = true
	if s.graceful != nil {
		s.graceful.Stop()
	}
	if s.gracefulReq != nil {
		s.gracefulReq.respond("stopped")
	}
	for _, request := range s.killWaiters {
		request.respond("stopped")
	}
}

func (s *supervisorSession) gracefulChannel() <-chan time.Time {
	if s.graceful == nil {
		return nil
	}
	return s.graceful.C
}

func (s *supervisorSession) handleRequest(request stopRequest) {
	if request.kill {
		s.killGroup(syscall.SIGKILL)
		s.killWaiters = append(s.killWaiters, request)
		return
	}
	s.killGroup(syscall.SIGTERM)
	if s.graceful != nil {
		s.graceful.Stop()
	}
	s.graceful = time.NewTimer(gracefulTimeout)
	copied := request
	s.gracefulReq = &copied
}

func (s *supervisorSession) killGroup(signal syscall.Signal) {
	if s.cmd.Process == nil {
		return
	}
	err := syscall.Kill(-s.cmd.Process.Pid, signal)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return
	}
}

func (s *supervisorSession) reportReady(message string) error {
	s.readyC = nil
	if message != "ready" {
		s.failStartup(blankReady(message))
		s.killGroup(syscall.SIGKILL)
		return fmt.Errorf("start daemon: %s", blankReady(message))
	}
	_, _ = fmt.Fprintln(s.startup, "ready")
	*s.sent = true
	s.readyReported = true
	return nil
}

func (s *supervisorSession) run() error {
	for {
		err := s.step()
		if err != nil || sessionStopped(s) {
			return err
		}
	}
}

func (s *supervisorSession) step() error {
	select {
	case message := <-s.readyC:
		return s.reportReady(message)
	case request := <-s.requests:
		s.handleRequest(request)
	case <-s.gracefulChannel():
		s.expireGraceful()
	case <-s.signals:
		if !s.readyReported {
			return errors.New("daemon supervisor stopped before readiness")
		}
		if s.gracefulReq == nil {
			s.handleRequest(stopRequest{})
		}
	case <-s.workerDead:
		return s.workerExited()
	}
	return nil
}

func (s *supervisorSession) workerExited() error {
	if !s.readyReported && readyPending(s.readyC) {
		message := blankReady(<-s.workerReady)
		s.failStartup(message)
		return fmt.Errorf("start daemon: %s", message)
	}
	s.finish()
	return nil
}

func sessionStopped(s *supervisorSession) bool {
	return s.reaped
}

func blankReady(message string) string {
	if message == "" {
		return "daemon exited before readiness"
	}
	return message
}

func readyPending(ch <-chan string) bool {
	return ch != nil
}

func rejectsStop(line string) bool {
	return notStop(line) && notKill(line)
}

func notStop(line string) bool {
	return line != "stop\n"
}

func notKill(line string) bool {
	return line != "kill\n"
}

func forcesStop(line string) bool {
	return line == "kill\n"
}

func acceptStops(listener net.Listener, requests chan<- stopRequest) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			_ = conn.SetDeadline(time.Now().Add(2 * lifecycleTimeout))
			line, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil || rejectsStop(line) {
				_ = conn.Close()
				return
			}
			result := make(chan string, 1)
			done := make(chan struct{})
			requests <- stopRequest{kill: forcesStop(line), result: result, done: done}
			message := <-result
			_, _ = io.WriteString(conn, message+"\n")
			_ = conn.Close()
			close(done)
		}()
	}
}

// Worker runs the daemon in a supervised process, reporting startup errors.
func Worker(ctx context.Context, repoPath string, ready, supervisor *os.File) error {
	subprocess.UseInheritedProcessGroup()
	go func() {
		_, _ = io.Copy(io.Discard, supervisor)
		_ = syscall.Kill(-os.Getpid(), syscall.SIGKILL)
	}()
	signaled := false
	err := runReady(ctx, repoPath, func() {
		_, _ = fmt.Fprintln(ready, "ready")
		signaled = true
	})
	if !signaled {
		if err == nil {
			err = errors.New("daemon stopped before readiness")
		}
		_, _ = fmt.Fprintln(ready, err)
	}
	_ = ready.Close()
	return err
}
