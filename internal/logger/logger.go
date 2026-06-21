package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
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

// Capture de la dernière erreur, pour que le dashboard puisse signaler un bot
// dysfonctionnel (process vivant mais qui échoue, ex. clé API invalide).
var (
	lastErrorMu  sync.Mutex
	lastErrorMsg string
	lastErrorAt  time.Time
	errorCount   int64
)

func recordError(msg string) {
	lastErrorMu.Lock()
	lastErrorMsg = msg
	lastErrorAt = time.Now()
	errorCount++
	lastErrorMu.Unlock()
}

// LastError retourne le dernier message d'erreur enregistré, son horodatage et
// le nombre total d'erreurs depuis le démarrage (count == 0 = aucune erreur).
func LastError() (msg string, at time.Time, count int64) {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	return lastErrorMsg, lastErrorAt, errorCount
}

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

func IsInitialized() bool {
	return debugLogger != nil && infoLogger != nil && warnLogger != nil && errorLogger != nil
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
	recordError(fmt.Sprint(v...))
}

func Errorf(format string, v ...interface{}) {
	if currentLevel <= ERROR {
		errorLogger.Printf(format, v...)
	}
	recordError(fmt.Sprintf(format, v...))
}

// Fatal logs an error and exits
func Fatal(v ...interface{}) {
	errorLogger.Fatal(v...)
}

func Fatalf(format string, v ...interface{}) {
	errorLogger.Fatalf(format, v...)
}
