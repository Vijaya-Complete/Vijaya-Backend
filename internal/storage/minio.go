package storage

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"vijaya-backend/internal/config"
)

type Client struct {
	mc        *minio.Client
	Bucket    string
	PublicURL string
}

func NewClient(cfg *config.Config) (*Client, error) {
	mc, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioUser, cfg.MinioPassword, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("creating minio client: %w", err)
	}
	ctx := context.Background()
	exists, err := mc.BucketExists(ctx, cfg.MinioBucket)
	if err != nil {
		return nil, fmt.Errorf("checking bucket: %w", err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.MinioBucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("creating bucket: %w", err)
		}
		log.Printf("✓ Created bucket: %s", cfg.MinioBucket)
	}
	log.Println("✓ MinIO connected")
	return &Client{mc: mc, Bucket: cfg.MinioBucket, PublicURL: cfg.MinioPublicURL}, nil
}

func (c *Client) Upload(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	_, err := c.mc.PutObject(ctx, c.Bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("uploading to minio: %w", err)
	}
	return fmt.Sprintf("%s/%s", c.PublicURL, key), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.Bucket, key, minio.RemoveObjectOptions{})
}

func StorageKey(fullURL, publicURLPrefix string) string {
	if len(fullURL) > len(publicURLPrefix)+1 {
		return fullURL[len(publicURLPrefix)+1:]
	}
	return fullURL
}
