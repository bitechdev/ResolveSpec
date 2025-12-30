package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/config"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/server"
	"github.com/bitechdev/ResolveSpec/pkg/testmodels"

	"github.com/bitechdev/ResolveSpec/pkg/resolvespec"
	"github.com/gorilla/mux"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func main() {
	// Load configuration
	cfgMgr := config.NewManager()
	if err := cfgMgr.Load(); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	cfg, err := cfgMgr.GetConfig()
	if err != nil {
		log.Fatalf("Failed to get configuration: %v", err)
	}

	// Initialize logger with configuration
	logger.Init(cfg.Logger.Dev)
	if cfg.Logger.Path != "" {
		logger.UpdateLoggerPath(cfg.Logger.Path, cfg.Logger.Dev)
	}
	logger.Info("ResolveSpec test server starting")
	logger.Info("Configuration loaded - Server will listen on: %s", cfg.Server.Addr)

	// Initialize database
	db, err := initDB(cfg)
	if err != nil {
		logger.Error("Failed to initialize database: %+v", err)
		os.Exit(1)
	}

	// Create router
	r := mux.NewRouter()

	// Initialize API handler using new API
	handler := resolvespec.NewHandlerWithGORM(db)

	// Create a new registry instance and register models
	registry := modelregistry.NewModelRegistry()
	testmodels.RegisterTestModels(registry)

	// Register models with handler
	models := testmodels.GetTestModels()
	modelNames := []string{"departments", "employees", "projects", "project_tasks", "documents", "comments"}
	for i, model := range models {
		handler.RegisterModel("public", modelNames[i], model)
	}

	// Setup routes using new SetupMuxRoutes function (without authentication)
	resolvespec.SetupMuxRoutes(r, handler, nil)

	// Create server manager
	mgr := server.NewManager()

	// Parse host and port from addr
	host := ""
	port := 8080
	if cfg.Server.Addr != "" {
		// Parse addr (format: ":8080" or "localhost:8080")
		if cfg.Server.Addr[0] == ':' {
			// Just port
			_, err := fmt.Sscanf(cfg.Server.Addr, ":%d", &port)
			if err != nil {
				logger.Error("Invalid server address: %s", cfg.Server.Addr)
				os.Exit(1)
			}
		} else {
			// Host and port
			_, err := fmt.Sscanf(cfg.Server.Addr, "%[^:]:%d", &host, &port)
			if err != nil {
				logger.Error("Invalid server address: %s", cfg.Server.Addr)
				os.Exit(1)
			}
		}
	}

	// Add server instance
	_, err = mgr.Add(server.Config{
		Name:            "api",
		Host:            host,
		Port:            port,
		Handler:         r,
		ShutdownTimeout: cfg.Server.ShutdownTimeout,
		DrainTimeout:    cfg.Server.DrainTimeout,
		ReadTimeout:     cfg.Server.ReadTimeout,
		WriteTimeout:    cfg.Server.WriteTimeout,
		IdleTimeout:     cfg.Server.IdleTimeout,
	})
	if err != nil {
		logger.Error("Failed to add server: %v", err)
		os.Exit(1)
	}

	// Start server with graceful shutdown
	logger.Info("Starting server on %s", cfg.Server.Addr)
	if err := mgr.ServeWithGracefulShutdown(); err != nil {
		logger.Error("Server failed: %v", err)
		os.Exit(1)
	}
}

func initDB(cfg *config.Config) (*gorm.DB, error) {
	// Configure GORM logger based on config
	logLevel := gormlog.Info
	if !cfg.Logger.Dev {
		logLevel = gormlog.Warn
	}

	newLogger := gormlog.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		gormlog.Config{
			SlowThreshold:             time.Second, // Slow SQL threshold
			LogLevel:                  logLevel,    // Log level
			IgnoreRecordNotFoundError: true,        // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      true,        // Don't include params in the SQL log
			Colorful:                  cfg.Logger.Dev,
		},
	)

	// Use database URL from config if available, otherwise use default SQLite
	dbURL := cfg.Database.URL
	if dbURL == "" {
		dbURL = "test.db"
	}

	// Create SQLite database
	db, err := gorm.Open(sqlite.Open(dbURL), &gorm.Config{Logger: newLogger, FullSaveAssociations: false})
	if err != nil {
		return nil, err
	}

	modelList := testmodels.GetTestModels()

	// Auto migrate schemas
	err = db.AutoMigrate(modelList...)
	if err != nil {
		return nil, err
	}

	return db, nil
}
