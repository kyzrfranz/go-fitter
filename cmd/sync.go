package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/args"
	"github.com/kyzrfranz/go-fitter/internal/db"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/dropbox"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/folder"
	"github.com/kyzrfranz/go-fitter/internal/retrievers/gridfs"
	syncer "github.com/kyzrfranz/go-fitter/pkg/syncer"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// fitBucketName is the GridFS bucket that stores raw FIT blobs for locally
// imported activities. The API server reads from the same bucket.
const fitBucketName = "fit_files"

var (
	mongoUri            string
	dbName              string
	workspace           string
	write               bool
	overwrite           bool
	sync                string
	source              string
	fromDate            string
	toDate              string
	dropboxAppKey       string
	dropboxAppSecret    string
	dropboxRefreshToken string
	Version             = "0.0.1"
)

func main() {

	flag.StringVar(&mongoUri, "mongo-uri", args.EnvOrDefault[string]("MONGO_URI", ""), "")
	flag.StringVar(&dbName, "database", args.EnvOrDefault[string]("DATABASE_NAME", "fitter"), "")
	flag.StringVar(&workspace, "workspace", args.EnvOrDefault[string]("WORKSPACE_PATH", "./"), "Path to workspace folder")
	flag.BoolVar(&write, "write", args.EnvOrDefault[bool]("WRITE_MODE", false), "Whether to write to database")
	flag.BoolVar(&overwrite, "overwrite", args.EnvOrDefault[bool]("OVERWRITE", false), "Whether to overwrite existing documents in the database")
	flag.StringVar(&sync, "sync", args.EnvOrDefault[string]("SYNC_MODE", "activities"), "What to sync: activities, health")
	flag.StringVar(&source, "source", args.EnvOrDefault[string]("SOURCE", "dropbox"), "Where to read activities from: dropbox, folder")
	flag.StringVar(&fromDate, "from", args.EnvOrDefault[string]("FROM_DATE", ""), "Folder import only: import activities on/after this date (YYYY-MM-DD)")
	flag.StringVar(&toDate, "to", args.EnvOrDefault[string]("TO_DATE", ""), "Folder import only: import activities on/before this date (YYYY-MM-DD)")

	//dropbox credentials
	flag.StringVar(&dropboxAppKey, "dropbox-app-key", args.EnvOrDefault[string]("DROPBOX_APP_KEY", ""), "Dropbox appKey")
	flag.StringVar(&dropboxAppSecret, "dropbox-app-secret", args.EnvOrDefault[string]("DROPBOX_APP_SECRET", ""), "Dropbox appSecret")
	flag.StringVar(&dropboxRefreshToken, "dropbox-refresh-token", args.EnvOrDefault[string]("DROPBOX_API_REFRESH_TOKEN", ""), "Dropbox API refresh token")

	flag.Parse()

	cli, err := db.NewV1MongoClient(db.WithUri(mongoUri))

	bailOnError(err)

	if sync == "activities" {
		syncActivities(cli)
	} else if sync == "health" {
		syncHealth(cli)
	} else {
		fmt.Printf("Unknown sync mode: %s\n", sync)
		os.Exit(1)
	}
}

func syncHealth(client *mongo.Client) {
	collection := client.Database(dbName).Collection("health")

	//localRetriever := folder.New(10, "json")
	retriever := dropbox.New(dropboxAppKey, dropboxAppSecret, dropboxRefreshToken, "json")
	sncr := syncer.NewHealthSyncer(collection, retriever, os.Stdout)
	err := sncr.Sync("/Apps/Health Auto Export/Health Auto Export/daily")
	//err := sncr.Sync("/Users/kyzrfranz/Library/Mobile Documents/iCloud~com~ifunography~HealthExport/Documents/dump")
	if err != nil {
		panic(err)
	}
}

func syncActivities(client *mongo.Client) {
	if source == "folder" {
		syncActivitiesFromFolder(client)
		return
	}

	activityRepo := db.NewItemsRepository[v1.Activity](client, dbName, "activities")
	retriever := dropbox.New(dropboxAppKey, dropboxAppSecret, dropboxRefreshToken, "zip")
	sncr := syncer.NewActivitySyncer(activityRepo, retriever, os.Stdout, false)
	err := sncr.Sync("/Apps/RunGap/export")
	if err != nil {
		panic(err)
	}
}

// syncActivitiesFromFolder imports raw .fit files from a local folder (e.g. an
// unpacked Garmin export). Only activities whose start time falls within the
// optional --from/--to range are imported; everything else (including
// non-activity .fit files) is skipped. Duplicates are skipped by content-hash id.
func syncActivitiesFromFolder(client *mongo.Client) {
	repo := db.NewItemsRepository[v1.Activity](client, dbName, "activities")
	from := parseDate("from", fromDate, false)
	to := parseDate("to", toDate, true)

	// Raw FIT bytes go into a GridFS bucket keyed by activity id so the chart
	// endpoint can rebuild records without the local source file.
	bucket := client.Database(dbName).GridFSBucket(options.GridFSBucket().SetName(fitBucketName))
	store := gridfs.New(bucket)

	retriever := folder.NewWithRange(-1, "fit", from, to)
	sncr := syncer.NewActivitySyncer(repo, retriever, os.Stdout, overwrite)
	sncr.SetBlobStore(store)

	fmt.Printf("Importing .fit files from %s into DB + GridFS bucket %q (from=%q to=%q)...\n",
		workspace, fitBucketName, fromDate, toDate)
	if err := sncr.Sync(workspace); err != nil {
		panic(err)
	}
}

// parseDate parses a YYYY-MM-DD flag. endOfDay makes the bound inclusive of the
// whole day by snapping to 23:59:59. Empty input yields a zero time (no bound).
func parseDate(name, value string, endOfDay bool) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		fmt.Printf("invalid --%s date %q, expected YYYY-MM-DD: %v\n", name, value, err)
		os.Exit(1)
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Second)
	}
	return t
}

func bailOnError(err error) {
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
