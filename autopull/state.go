package autopull

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type RuntimeState struct {
	Pulls             int       `json:"pulls"`
	LastPull          time.Time `json:"last_pull"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
	BackoffUntil      time.Time `json:"backoff_until"`
	LastError         string    `json:"last_error"`
}

type repoState struct {
	consecutiveErrors int
	backoffUntil      time.Time
}

func pidFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.pid")
}

func stateFilePath(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), ".auto_pull.state.json")
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func processStopped(pid int) bool {
	if runtime.GOOS != "linux" || pid <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		return false
	}
	// Linux process state: 'T' = stopped (job control), 't' = tracing stop.
	state := parts[2]
	return state == "T" || state == "t"
}

func processRunning(pid int) bool {
	if !processAlive(pid) {
		return false
	}
	return !processStopped(pid)
}

func stopProcess(pidPath string) (string, error) {
	pid, err := readPID(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("pid file not found: %s", pidPath), nil
		}
		return "", err
	}
	if !processAlive(pid) {
		_ = os.Remove(pidPath)
		return fmt.Sprintf("Process %d not running; cleaned pid file", pid), nil
	}
	if processStopped(pid) {
		// A suspended process will not handle SIGTERM until resumed.
		_ = syscall.Kill(pid, syscall.SIGKILL)
		_ = os.Remove(pidPath)
		return fmt.Sprintf("Process %d was suspended; sent SIGKILL and cleaned pid file", pid), nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return "", err
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = os.Remove(pidPath)
			return fmt.Sprintf("Stopped process %d", pid), nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		return "", err
	}
	_ = os.Remove(pidPath)
	return fmt.Sprintf("Process %d did not stop with SIGTERM; sent SIGKILL", pid), nil
}

func loadRuntimeState(path string) *RuntimeState {
	data, err := os.ReadFile(path)
	if err != nil {
		return &RuntimeState{}
	}
	var rs RuntimeState
	if err := json.Unmarshal(data, &rs); err != nil {
		return &RuntimeState{}
	}
	return &rs
}

func saveRuntimeState(path string, rs *RuntimeState) error {
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func TailFile(path string, lines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		buf = append(buf, scanner.Text())
		if len(buf) > lines {
			buf = buf[1:]
		}
	}
	return strings.Join(buf, "\n"), nil
}

func BackoffDuration(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	const maxDuration = 5 * time.Minute
	shift := uint(failures - 1)
	if shift >= 9 { // 2^9 s = 512 s > 5 min
		return maxDuration
	}
	d := time.Second << shift
	if d > maxDuration {
		return maxDuration
	}
	return d
}
