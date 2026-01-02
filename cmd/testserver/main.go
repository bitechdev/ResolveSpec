package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/config"
	"github.com/bitechdev/ResolveSpec/pkg/dbmanager"
	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/server"
	"github.com/bitechdev/ResolveSpec/pkg/testmodels"

	"github.com/bitechdev/ResolveSpec/pkg/resolvespec"
	"github.com/gorilla/mux"

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

	// Initialize database manager
	ctx := context.Background()
	dbMgr, db, err := initDB(ctx, cfg)
	if err != nil {
		logger.Error("Failed to initialize database: %+v", err)
		os.Exit(1)
	}
	defer dbMgr.Close()

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

	// Get default server configuration
	defaultServerCfg, err := cfg.Servers.GetDefault()
	if err != nil {
		logger.Error("Failed to get default server config: %v", err)
		os.Exit(1)
	}

	// Apply global defaults
	defaultServerCfg.ApplyGlobalDefaults(cfg.Servers)

	// Convert to server.Config and add instance
	serverCfg := server.FromConfigInstanceToServerConfig(defaultServerCfg, r)

	logger.Info("Configuration loaded - Server '%s' will listen on %s:%d",
		serverCfg.Name, serverCfg.Host, serverCfg.Port)

	_, err = mgr.Add(serverCfg)
	if err != nil {
		logger.Error("Failed to add server: %v", err)
		os.Exit(1)
	}

	// Start server with graceful shutdown
	logger.Info("Starting server '%s' on %s:%d", serverCfg.Name, serverCfg.Host, serverCfg.Port)
	if err := mgr.ServeWithGracefulShutdown(); err != nil {
		logger.Error("Server failed: %v", err)
		os.Exit(1)
	}
}

func initDB(ctx context.Context, cfg *config.Config) (dbmanager.Manager, *gorm.DB, error) {
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

	// Create database manager from config
	mgr, err := dbmanager.NewManager(dbmanager.FromConfig(cfg.DBManager))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create database manager: %w", err)
	}

	// Connect all databases
	if err := mgr.Connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to connect databases: %w", err)
	}

	// Get default connection
	conn, err := mgr.GetDefault()
	if err != nil {
		mgr.Close()
		return nil, nil, fmt.Errorf("failed to get default connection: %w", err)
	}

	// Get GORM database
	gormDB, err := conn.GORM()
	if err != nil {
		mgr.Close()
		return nil, nil, fmt.Errorf("failed to get GORM database: %w", err)
	}

	// Update GORM logger
	gormDB.Logger = newLogger

	modelList := testmodels.GetTestModels()

	// Auto migrate schemas
	if err := gormDB.AutoMigrate(modelList...); err != nil {
		mgr.Close()
		return nil, nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	return mgr, gormDB, nil
}
