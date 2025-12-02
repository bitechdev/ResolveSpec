package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/modelregistry"
	"github.com/bitechdev/ResolveSpec/pkg/testmodels"

	"github.com/bitechdev/ResolveSpec/pkg/resolvespec"
	"github.com/gorilla/mux"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func main() {
	// Initialize logger
	logger.Init(true)
	logger.Info("ResolveSpec test server starting")

	// Initialize database
	db, err := initDB()
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

	// Start server
	logger.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		logger.Error("Server failed to start: %v", err)
		os.Exit(1)
	}
}

func initDB() (*gorm.DB, error) {

	newLogger := gormlog.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		gormlog.Config{
			SlowThreshold:             time.Second,  // Slow SQL threshold
			LogLevel:                  gormlog.Info, // Log level
			IgnoreRecordNotFoundError: true,         // Ignore ErrRecordNotFound error for logger
			ParameterizedQueries:      true,         // Don't include params in the SQL log
			Colorful:                  true,         // Disable color
		},
	)

	// Create SQLite database
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{Logger: newLogger, FullSaveAssociations: false})
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
