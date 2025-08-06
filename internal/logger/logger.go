package logger

import (
	"io"
	"log"
	"os"
	"strings"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var (
	currentLevel LogLevel = INFO
	debugLogger  *log.Logger
	infoLogger   *log.Logger
	warnLogger   *log.Logger
	errorLogger  *log.Logger
)

func InitLogger(level string, logFile string) error {
	// Déterminer le niveau
	switch strings.ToLower(level) {
	case "debug":
		currentLevel = DEBUG
	case "info":
		currentLevel = INFO
	case "warn":
		currentLevel = WARN
	case "error":
		currentLevel = ERROR
	}

	// Déterminer la sortie
	var output io.Writer = os.Stdout

	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return err
		}
		// Écrire à la fois dans le fichier et stdout
		output = io.MultiWriter(os.Stdout, file)
	}

	// Créer les loggers
	debugLogger = log.New(output, "[DEBUG] ", log.LstdFlags|log.Lshortfile)
	infoLogger = log.New(output, "[INFO]  ", log.LstdFlags)
	warnLogger = log.New(output, "[WARN]  ", log.LstdFlags)
	errorLogger = log.New(output, "[ERROR] ", log.LstdFlags|log.Lshortfile)

	return nil
}

func Debug(v ...interface{}) {
	if currentLevel <= DEBUG {
		debugLogger.Println(v...)
	}
}

func Debugf(format string, v ...interface{}) {
	if currentLevel <= DEBUG {
		debugLogger.Printf(format, v...)
	}
}

func Info(v ...interface{}) {
	if currentLevel <= INFO {
		infoLogger.Println(v...)
	}
}

func Infof(format string, v ...interface{}) {
	if currentLevel <= INFO {
		infoLogger.Printf(format, v...)
	}
}

func Warn(v ...interface{}) {
	if currentLevel <= WARN {
		warnLogger.Println(v...)
	}
}

func Warnf(format string, v ...interface{}) {
	if currentLevel <= WARN {
		warnLogger.Printf(format, v...)
	}
}

func Error(v ...interface{}) {
	if currentLevel <= ERROR {
		errorLogger.Println(v...)
	}
}

func Errorf(format string, v ...interface{}) {
	if currentLevel <= ERROR {
		errorLogger.Printf(format, v...)
	}
}

// Fatal logs an error and exits
func Fatal(v ...interface{}) {
	errorLogger.Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	errorLogger.Fatalf(format, v...)
}
