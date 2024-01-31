package log

/*
	log module is used to write log info to log file
	open a log file when log was created, and close it when log was destroyed
*/

import (
	"fmt"
	go_log "log"
	"os"
	"time"
)

type Log struct {
	Level int
	//File of log
	File *os.File
	path string
}

const (
	LOG_LEVEL_DEBUG = 0
	LOG_LEVEL_INFO  = 1
	LOG_LEVEL_WARN  = 2
	LOG_LEVEL_ERROR = 3
)

func (l *Log) Debug(format string, stdout bool, v ...interface{}) {
	if l.Level <= LOG_LEVEL_DEBUG {
		l.writeLog("DEBUG", format, stdout, v...)
	}
}

func (l *Log) Info(format string, stdout bool, v ...interface{}) {
	if l.Level <= LOG_LEVEL_INFO {
		l.writeLog("INFO", format, stdout, v...)
	}
}

func (l *Log) Warn(format string, stdout bool, v ...interface{}) {
	if l.Level <= LOG_LEVEL_WARN {
		l.writeLog("WARN", format, stdout, v...)
	}
}

func (l *Log) Error(format string, stdout bool, v ...interface{}) {
	if l.Level <= LOG_LEVEL_ERROR {
		l.writeLog("ERROR", format, stdout, v...)
	}
}

func (l *Log) Panic(format string, stdout bool, v ...interface{}) {
	l.writeLog("PANIC", format, stdout, v...)
	panic("")
}

func (l *Log) writeLog(level string, format string, stdout bool, v ...interface{}) {
	//if the next day is coming, reopen file
	if time.Now().Format("/2006-01-02.log") != l.File.Name() {
		l.File.Close()
		l.OpenFile()
	}
	//test if file is closed
	if l.File == nil {
		//open file
		err := l.OpenFile()
		if err != nil {
			panic(err)
		}
	}
	//write log
	format = fmt.Sprintf("["+level+"]"+format, v...)

	if show_log && stdout {
		logger.Output(4, format)
	}

	_, err := l.File.Write([]byte(format + "\n"))
	if err != nil {
		//reopen file
		l.File.Close()
		l.OpenFile()
	}
}

func (l *Log) SetLogLevel(level int) {
	l.Level = level
}

func (l *Log) OpenFile() error {
	//test if file is closed
	if l.File == nil {
		//open file
		file, err := os.OpenFile(l.path+time.Now().Format("/2006-01-02.log"), os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		l.File = file
	}
	//test if file is writable
	_, err := l.File.Write([]byte(" "))
	if err != nil {
		//reopen file
		l.File.Close()
		file, err := os.OpenFile(l.path+time.Now().Format("/2006-01-02.log"), os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		l.File = file
	}
	return nil
}

func NewLog(path string) (*Log, error) {
	if path == "" {
		path = "log"
	}
	//test if path is exist
	_, err := os.Stat(path)
	if err != nil {
		//create path
		err = os.MkdirAll(path, 0777)
		if err != nil {
			return nil, err
		}
	}
	// test if path is a directory
	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("log file path %s is not a directory", path)
	}

	log := &Log{
		Level: LOG_LEVEL_DEBUG,
		path:  path,
	}
	//open file
	err = log.OpenFile()
	if err != nil {
		return nil, err
	}
	return log, nil
}

func init() {
	// why logger will cause panic when call
	initlog()
}

func initlog() {
	var err error
	main_log, err = NewLog("./logs")
	if err != nil {
		panic(err)
	}
}

var main_log *Log // wapper of go_log
var show_log bool = true
var logger = go_log.New(os.Stdout, "", go_log.Ldate|go_log.Ltime|go_log.Lshortfile)

func SetShowLog(show bool) {
	show_log = show
}

func SetLogLevel(level int) {
	if main_log == nil {
		initlog()
	}
	main_log.SetLogLevel(level)
}

func Debug(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Debug(format, true, v...)
}

func Info(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Info(format, true, v...)
}

func Warn(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Warn(format, true, v...)
}

func Error(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Error(format, true, v...)
}

func Panic(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Panic(format, true, v...)
}

func SlientDebug(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Debug(format, false, v...)
}

func SlientInfo(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Info(format, false, v...)
}

func SlientWarn(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Warn(format, false, v...)
}

func SlientError(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Error(format, false, v...)
}

func SlientPanic(format string, v ...interface{}) {
	if main_log == nil {
		initlog()
	}
	main_log.Panic(format, false, v...)
}
