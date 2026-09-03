# Health / Fit Syncer

This tool can be used to consolidate health data (currently only AppleHealthkit) and Activity data in the form of .FIT files
into one database (currently mongodb).

As core prerequisites, you need:
- A running MongoDB instance
- Exported Apple Health Data as json, either in a local folder or on a dropboc account
- Exported .FIT files from your fitness device either in a local folder or on a dropbox account
- A Dropbox API token if you want to read data from dropbox

Then you can run the tool with the following parameters:

```bash
go run cmd/main.go \
  --mongo-uri "mongodb://localhost:27017" \               
  --mongo-database "healthsync" \                         
  --workspace "points/to/a/folder" \                                        
  --dropbox-api-refresh-token "your_dropbox_api_refresh_token" \
  --dropbox-app-key "your_dropbox_app_key" \
  --dropbox-app-secret "your_dropbox_app_secret" \
  --sync health
```

or 

```bash
go run cmd/main.go \
  --mongo-uri "mongodb://localhost:27017" \               
  --mongo-database "healthsync" \                         
  --workspace "points/to/a/folder" \                                        
  --dropbox-api-refresh-token "your_dropbox_api_refresh_token" \
  --dropbox-app-key "your_dropbox_app_key" \
  --dropbox-app-secret "your_dropbox_app_secret" \
  --sync activity
```

*Hint:* You can also set all the args as `ENV` variables