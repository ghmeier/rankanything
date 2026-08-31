package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Storage interface {
	Upload(ctx context.Context, key string, r io.Reader, contentType string) (url string, err error)
	Delete(ctx context.Context, key string) error
}

type R2Config struct {
	AccountID      string
	AccessKeyID    string
	SecretAccessKey string
	BucketName     string
	PublicURL      string
}

type R2Storage struct {
	client    *s3.Client
	bucket    string
	publicURL string
}

func NewR2Storage(ctx context.Context, cfg R2Config) (*R2Storage, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID, cfg.SecretAccessKey, "",
		)),
		awsconfig.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	return &R2Storage{
		client:    client,
		bucket:    cfg.BucketName,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}, nil
}

func (s *R2Storage) Upload(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("storage: upload %s: %w", key, err)
	}
	return s.publicURL + "/" + key, nil
}

func (s *R2Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

type LocalStorage struct {
	dir         string
	servePrefix string
}

func NewLocalStorage(dir, servePrefix string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create dir %s: %w", dir, err)
	}
	return &LocalStorage{
		dir:         dir,
		servePrefix: "/" + strings.Trim(servePrefix, "/"),
	}, nil
}

var errPathTraversal = fmt.Errorf("storage: key escapes base directory")

func (s *LocalStorage) safePath(key string) (string, error) {
	path := filepath.Join(s.dir, filepath.FromSlash(key))
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(s.dir)+string(filepath.Separator)) {
		return "", errPathTraversal
	}
	return path, nil
}

func (s *LocalStorage) Upload(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	path, err := s.safePath(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("storage: create dirs for %s: %w", key, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("storage: create %s: %w", key, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("storage: write %s: %w", key, err)
	}

	return s.servePrefix + "/" + key, nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

func (s *LocalStorage) ServeHandler() http.Handler {
	return http.StripPrefix(s.servePrefix, http.FileServer(http.Dir(s.dir)))
}

type MemoryStorage struct {
	Files map[string][]byte
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{Files: make(map[string][]byte)}
}

func (m *MemoryStorage) Upload(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	m.Files[key] = data
	return "/uploads/" + key, nil
}

func (m *MemoryStorage) Delete(_ context.Context, key string) error {
	delete(m.Files, key)
	return nil
}
