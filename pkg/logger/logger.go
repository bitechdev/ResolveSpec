package logger

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/bitechdev/ResolveSpec/pkg/errortracking"
	"go.uber.org/zap"
)

var Logger *zap.SugaredLogger
var errorTracker errortracking.Provider

func Init(dev bool) {

	if dev {
		cfg := zap.NewDevelopmentConfig()
		UpdateLogger(&cfg)
	} else {
		cfg := zap.NewProductionConfig()
		UpdateLogger(&cfg)
	}

}

func UpdateLoggerPath(path string, dev bool) {
	defaultConfig := zap.NewProductionConfig()
	if dev {
		defaultConfig = zap.NewDevelopmentConfig()
	}
	defaultConfig.OutputPaths = []string{path}
	UpdateLogger(&defaultConfig)
}

func UpdateLogger(config *zap.Config) {
	defaultConfig := zap.NewProductionConfig()
	defaultConfig.OutputPaths = []string{"resolvespec.log"}
	if config == nil {
		config = &defaultConfig
	}

	logger, err := config.Build()
	if err != nil {
		log.Print(err)
		return
	}

	Logger = logger.Sugar()
	Info("ResolveSpec Logger initialized")
}

// InitErrorTracking initializes the error tracking provider
func InitErrorTracking(provider errortracking.Provider) {
	errorTracker = provider
	if errorTracker != nil {
		Info("Error tracking initialized")
	}
}

// GetErrorTracker returns the current error tracking provider
func GetErrorTracker() errortracking.Provider {
	return errorTracker
}

// CloseErrorTracking flushes and closes the error tracking provider
func CloseErrorTracking() error {
	if errorTracker != nil {
		errorTracker.Flush(5)
		return errorTracker.Close()
	}
	return nil
}

func Info(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)
		return
	}
	Logger.Infow(fmt.Sprintf(template, args...), "process_id", os.Getpid())
}

func Warn(template string, args ...interface{}) {
	message := fmt.Sprintf(template, args...)
	if Logger == nil {
		log.Printf("%s", message)
	} else {
		Logger.Warnw(message, "process_id", os.Getpid())
	}

	// Send to error tracker
	if errorTracker != nil {
		errorTracker.CaptureMessage(context.Background(), message, errortracking.SeverityWarning, map[string]interface{}{
			"process_id": os.Getpid(),
		})
	}
}

func Error(template string, args ...interface{}) {
	message := fmt.Sprintf(template, args...)
	if Logger == nil {
		log.Printf("%s", message)
	} else {
		Logger.Errorw(message, "process_id", os.Getpid())
	}

	// Send to error tracker
	if errorTracker != nil {
		errorTracker.CaptureMessage(context.Background(), message, errortracking.SeverityError, map[string]interface{}{
			"process_id": os.Getpid(),
		})
	}
}

func Debug(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)
		return
	}
	Logger.Debugw(fmt.Sprintf(template, args...), "process_id", os.Getpid())
}

// CatchPanic - Handle panic
func CatchPanicCallback(location string, cb func(err any)) {
	if err := recover(); err != nil {
		callstack := debug.Stack()

		if Logger != nil {
			Error("Panic in %s : %v", location, err)
		} else {
			fmt.Printf("%s:PANIC->%+v", location, err)
			debug.PrintStack()
		}

		// Send to error tracker
		if errorTracker != nil {
			errorTracker.CapturePanic(context.Background(), err, callstack, map[string]interface{}{
				"location":   location,
				"process_id": os.Getpid(),
			})
		}

		if cb != nil {
			cb(err)
		}
	}
}

// CatchPanic - Handle panic
func CatchPanic(location string) {
	CatchPanicCallback(location, nil)
}

// HandlePanic logs a panic and returns it as an error
// This should be called with the result of recover() from a deferred function
// Example usage:
//
//	defer func() {
//	    if r := recover(); r != nil {
//	        err = logger.HandlePanic("MethodName", r)
//	    }
//	}()
func HandlePanic(methodName string, r any) error {
	stack := debug.Stack()
	Error("Panic in %s: %v\nStack trace:\n%s", methodName, r, string(stack))

	// Send to error tracker
	if errorTracker != nil {
		errorTracker.CapturePanic(context.Background(), r, stack, map[string]interface{}{
			"method":     methodName,
			"process_id": os.Getpid(),
		})
	}

	return fmt.Errorf("panic in %s: %v", methodName, r)
}
