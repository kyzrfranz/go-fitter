package main

import (
	"flag"
	"log/slog"
	stdhttp "net/http"
	"os"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/args"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/internal/http"
	mcpsrv "github.com/kyzrfranz/go-fitter/internal/mcp"
	restActivity "github.com/kyzrfranz/go-fitter/internal/rest/activity"
	restHandler "github.com/kyzrfranz/go-fitter/internal/rest/handler"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/dropbox"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/gridfs"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	logger              *slog.Logger
	serverPort          = 0
	mongoUri            string
	dbName              string
	dropboxAppKey       string
	dropboxAppSecret    string
	dropboxRefreshToken string
	Version             = "0.0.1"
)

func main() {

	flag.IntVar(&serverPort, "port", args.EnvOrDefault[int]("SERVER_PORT", 8080), "Port for the API server")
	flag.StringVar(&mongoUri, "mongo-uri", args.EnvOrDefault[string]("MONGO_URI", ""), "")
	flag.StringVar(&dbName, "database", args.EnvOrDefault[string]("DATABASE_NAME", "fitter"), "")
	flag.StringVar(&dropboxAppKey, "dropbox-app-key", args.EnvOrDefault[string]("DROPBOX_APP_KEY", ""), "")
	flag.StringVar(&dropboxAppSecret, "dropbox-app-secret", args.EnvOrDefault[string]("DROPBOX_APP_SECRET", ""), "")
	flag.StringVar(&dropboxRefreshToken, "dropbox-refresh-token", args.EnvOrDefault[string]("DROPBOX_API_REFRESH_TOKEN", ""), "")

	flag.Parse()

	cli, err := db.NewV1MongoClient(db.WithUri(mongoUri))
	if err != nil {
		panic(err)
	}

	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	apiServer := http.NewApiServer(serverPort, logger)

	apiServer.Use(http.MiddlewareRecovery)
	apiServer.Use(http.MiddlewareCORS)
	apiServer.Use(http.MiddlewareLogging(logger))

	// Used by /activities/{id}/series to re-read the raw FIT on demand.
	// Dropbox-sourced activities read from dropbox; locally-imported ones read
	// their raw FIT back from GridFS. The dispatcher routes by Source.Path.
	var fallback gridfs.RawReader
	if dropboxAppKey != "" && dropboxAppSecret != "" && dropboxRefreshToken != "" {
		fallback = dropbox.New(dropboxAppKey, dropboxAppSecret, dropboxRefreshToken, "zip")
	}
	store := gridfs.New(cli.Database(dbName).GridFSBucket(options.GridFSBucket().SetName("fit_files")))
	var retriever restActivity.RawReader = gridfs.NewDispatcher(store, fallback)

	setupHandlers(apiServer, cli, retriever)

	apiServer.Start()
}

func setupHandlers(apiServer *http.ApiServer, client *mongo.Client, retriever restActivity.RawReader) {
	handler := restHandler.NewHandler(logger, client, dbName, retriever)

	//apiServer.AddHandler("/fit", handler.Fit)
	apiServer.AddHandler("/health", handler.Health.List)
	apiServer.AddHandler("/activities/{id}/series", handler.Activity.Series)
	apiServer.AddHandler("/activities/{id}", handler.Activity.Get)
	apiServer.AddHandler("/activities", handler.Activity.List)
	apiServer.AddHandler("/llm-context", handler.LLMContext.List)
	apiServer.AddHandler("/openapi.yaml", serveOpenAPISpec)

	mcpHandler := mcpsrv.NewHandler(mcpsrv.Deps{
		Activities: db.NewItemsRepository[v1.Activity](client, dbName, "activities"),
		Health:     db.NewItemsRepository[db.HealthMetric](client, dbName, "health"),
		Retriever:  retriever,
	})
	apiServer.AddHandler("/mcp", mcpHandler.ServeHTTP)
}

func serveOpenAPISpec(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(v1.OpenAPISpec)
}
