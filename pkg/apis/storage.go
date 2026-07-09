package apis

import (
	"path/filepath"
	"sync"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/files"
)

var storageCache = struct {
	sync.Mutex
	app *core.App
	st  files.Storage
	sig string
}{}

// StorageFromApp returns the configured blob storage (local by default,
// S3 when storage.adapter=s3). The instance is cached per configuration.
func StorageFromApp(app *core.App) (files.Storage, error) {
	s := app.Settings()
	sig := s.String("storage.adapter") + "|" + s.String("storage.s3.endpoint") + "|" +
		s.String("storage.s3.region") + "|" + s.String("storage.s3.bucket") + "|" +
		s.String("storage.s3.accessKey") + "|" + s.String("storage.s3.secretKey")

	storageCache.Lock()
	defer storageCache.Unlock()
	if storageCache.app == app && storageCache.sig == sig && storageCache.st != nil {
		return storageCache.st, nil
	}

	var st files.Storage
	var err error
	if s.String("storage.adapter") == "s3" {
		st, err = files.NewS3(
			s.String("storage.s3.endpoint"),
			s.String("storage.s3.region"),
			s.String("storage.s3.bucket"),
			s.String("storage.s3.accessKey"),
			s.String("storage.s3.secretKey"),
		)
	} else {
		st, err = files.NewLocal(filepath.Join(app.Config().DataDir, "storage"))
	}
	if err != nil {
		return nil, err
	}
	storageCache.app, storageCache.st, storageCache.sig = app, st, sig
	return st, nil
}

func registerStorageSettings(app *core.App) {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "storage", Title: "File storage", Order: 30,
		Fields: []core.SettingsField{
			{Key: "storage.adapter", Label: "Adapter", Type: "select", Options: []string{"local", "s3"}, Default: "local"},
			{Key: "storage.s3.endpoint", Label: "S3 endpoint", Type: "text",
				Help: "Leave empty for AWS; set for MinIO/R2 (e.g. https://minio.local:9000)."},
			{Key: "storage.s3.region", Label: "S3 region", Type: "text", Placeholder: "eu-central-1"},
			{Key: "storage.s3.bucket", Label: "S3 bucket", Type: "text"},
			{Key: "storage.s3.accessKey", Label: "S3 access key", Type: "text"},
			{Key: "storage.s3.secretKey", Label: "S3 secret key", Type: "secret"},
		},
	})
}
