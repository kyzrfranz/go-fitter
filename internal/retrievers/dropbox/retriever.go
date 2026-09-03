package dropbox

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox"
	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"golang.org/x/oauth2"
)

type Retriever struct {
	config   dropbox.Config
	fileType string
}

func New(appKey, appSecret, refreshToken string, fileType string) *Retriever {
	conf := &oauth2.Config{
		ClientID:     appKey,
		ClientSecret: appSecret,
		Endpoint:     dropbox.OAuthEndpoint(""),
		Scopes:       []string{"files.content.read", "files.metadata.read", "account_info.read"}, // Add scopes as needed
	}

	initialToken := &oauth2.Token{
		RefreshToken: refreshToken,
	}
	ctx := context.Background()
	tokenSource := conf.TokenSource(ctx, initialToken)
	httpClient := oauth2.NewClient(ctx, tokenSource)

	return &Retriever{
		config: dropbox.Config{
			Client: httpClient,
		},
		fileType: strings.ToLower(fileType),
	}
}

func (d *Retriever) Retrieve(path string) ([]string, error) {
	dbf := files.New(d.config)
	arg := files.NewListFolderArg(path)
	arg.Recursive = false
	arg.IncludeMediaInfo = false
	arg.IncludeDeleted = false
	arg.Limit = 2000 // Maximize batch size to reduce API calls

	res, err := dbf.ListFolder(arg)
	if err != nil {
		return nil, err
	}

	// Create an intermediate struct to hold the time.Time object for sorting
	type fileEntry struct {
		path      string
		clientMod time.Time
	}
	var rawEntries []fileEntry

	// Helper to filter valid files from a batch of results
	filter := func(items []files.IsMetadata) {
		for _, item := range items {
			// Type assert to ensure it's a File (not a Folder)
			if f, ok := item.(*files.FileMetadata); ok {
				// Case-insensitive check for extension
				if strings.HasSuffix(strings.ToLower(f.Name), d.fileType) {
					rawEntries = append(rawEntries, fileEntry{
						path:      f.PathLower,
						clientMod: f.ClientModified,
					})
				}
			}
		}
	}

	// Filter first batch
	filter(res.Entries)

	// Pagination loop
	for res.HasMore {
		contArg := files.NewListFolderContinueArg(res.Cursor)
		res, err = dbf.ListFolderContinue(contArg)
		if err != nil {
			return nil, err
		}
		// Filter subsequent batches
		filter(res.Entries)
	}

	// Sort descending (newest files at index 0)
	sort.Slice(rawEntries, func(i, j int) bool {
		return rawEntries[i].clientMod.After(rawEntries[j].clientMod)
	})

	// Extract just the paths to return the standard []string
	var entries []string
	for _, entry := range rawEntries {
		entries = append(entries, entry.path)
	}

	return entries, nil
}

func (d *Retriever) Read(remotePath string) (io.ReadCloser, error) {
	dbf := files.New(d.config)
	// The Download method returns (FileMetadata, io.ReadCloser, error)
	_, content, err := dbf.Download(files.NewDownloadArg(remotePath))
	if err != nil {
		return nil, err
	}
	if d.fileType == "json" {
		return content, nil
	} else if d.fileType == "zip" {
		fitFile, err := retrieveFitFromZip(content)
		if err != nil {
			return nil, err
		}

		if fitFile == nil {
			return nil, fmt.Errorf("no fit found in zip")
		}

		activity, err := getActivity(fitFile, true, remotePath)
		if err != nil {
			return nil, err
		}
		return activity, nil
	}
	return nil, nil
}
