package autopull

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

type logger struct {
	file   *os.File
	logger *log.Logger
}

func newLogger(logPath string) (*logger, error) {
	if err := RotateIfLarge(logPath, getLogMaxBytes()); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &logger{
		file:   f,
		logger: log.New(f, "", 0),
	}, nil
}

func (l *logger) log(level, msg string) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s", ts, level, msg)
	fmt.Println(line)
	l.logger.Println(line)
}

func (l *logger) info(msg string)   { l.log("INFO ", msg) }
func (l *logger) ok(msg string)     { l.log("OK   ", msg) }
func (l *logger) warn(msg string)   { l.log("WARN ", msg) }
func (l *logger) errLog(msg string) { l.log("ERROR", msg) }
func (l *logger) close()            { l.file.Close() }

func RotateIfLarge(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < maxBytes {
		return nil
	}
	backup := path + ".1"
	_ = os.Remove(backup)
	return os.Rename(path, backup)
}

func getLogMaxBytes() int64 {
	const defaultSize = int64(5 * 1024 * 1024) // 5 MB
	raw := os.Getenv("AUTOPULL_LOG_MAX_BYTES")
	if raw == "" {
		return defaultSize
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return defaultSize
	}
	return v
}
