package logger

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"go.uber.org/zap"
)

var Logger *zap.SugaredLogger

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

func Info(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)
		return
	}
	Logger.Infow(fmt.Sprintf(template, args...), "process_id", os.Getpid())
}

func Warn(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)
		return
	}
	Logger.Warnw(fmt.Sprintf(template, args...), "process_id", os.Getpid())
}

func Error(template string, args ...interface{}) {
	if Logger == nil {
		log.Printf(template, args...)
		return
	}
	Logger.Errorw(fmt.Sprintf(template, args...), "process_id", os.Getpid())
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
		// callstack := debug.Stack()

		if Logger != nil {
			Error("Panic in %s : %v", location, err)
		} else {
			fmt.Printf("%s:PANIC->%+v", location, err)
			debug.PrintStack()
		}

		// push to sentry
		// hub := sentry.CurrentHub()
		// if hub != nil {
		// 	evtID := hub.Recover(err)
		// 	if evtID != nil {
		// 		sentry.Flush(time.Second * 2)
		// 	}
		// }

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
	return fmt.Errorf("panic in %s: %v", methodName, r)
}
