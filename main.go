package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/kyzrfranz/go-fitter/internal/args"
	"github.com/kyzrfranz/go-fitter/internal/http"
	"github.com/kyzrfranz/go-fitter/internal/rest"
)

var (
	logger     *slog.Logger
	serverPort = 0
)

func main() {

	flag.IntVar(&serverPort, "port", args.EnvOrDefault[int]("SERVER_PORT", 8080), "Port for the API server")

	flag.Parse()

	logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	apiServer := http.NewApiServer(serverPort, logger)

	apiServer.Use(http.MiddlewareRecovery)
	apiServer.Use(http.MiddlewareCORS)
	apiServer.Use(http.MiddlewareLogging(logger))

	setupHandlers(apiServer)

	apiServer.Start()
}

func setupHandlers(apiServer *http.ApiServer) {
	handler := rest.NewHandler(logger)

	apiServer.AddHandler("/fit", handler.Fit)
}
