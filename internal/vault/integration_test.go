package vault

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestIntegrationS3PrivateArchiveIntegrityAndCleanup(t *testing.T) {
	endpoint := os.Getenv("INTEGRATION_S3_ENDPOINT")
	if endpoint == "" {
		if os.Getenv("INTEGRATION_REQUIRED") == "1" {
			t.Fatal("INTEGRATION_S3_ENDPOINT required")
		}
		t.Skip("set INTEGRATION_S3_ENDPOINT to run S3 fixture")
	}
	bucket := "wppgo-integration-" + uuid.NewString()
	objects, err := NewS3(endpoint, "integration", "integration-only-password", bucket, "us-east-1", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = objects.Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	defer objects.Client.RemoveBucket(context.Background(), bucket)
	store := historyStore(t)
	v := &Vault{Objects: objects, Retention: time.Hour}
	chat := "123@s.whatsapp.net"
	data := []byte("private integration media")
	meta, err := v.Archive(ctx, store, "alpha", chat, "integration", "text/plain", data)
	if err != nil {
		t.Fatal(err)
	}
	defer objects.Delete(context.Background(), meta.Key)
	_, got, err := v.Download(ctx, store, "alpha", chat, "integration")
	if err != nil || string(got) != string(data) {
		t.Fatalf("S3 roundtrip: %q %v", got, err)
	}
	request, err := http.NewRequestWithContext(ctx, "GET", "http://"+endpoint+"/"+bucket+"/"+meta.Key, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("anonymous object access: HTTP %d", response.StatusCode)
	}
	meta.Expires = time.Now().Add(-time.Second).UnixMilli()
	if err = store.FinishMedia(ctx, meta); err != nil {
		t.Fatal(err)
	}
	if err = v.Cleanup(ctx, store, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err = objects.Client.StatObject(ctx, bucket, meta.Key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("expired object still exists")
	}
}
