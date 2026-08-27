package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type S3Config struct {
	Endpoint   string
	Region     string
	Bucket     string
	AccessKey  string
	SecretKey  string
	PathStyle  bool
	DisableTLS bool
}

type S3Store struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewS3(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Region == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 region and bucket are required")
	}
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKey != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load object-store configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.PathStyle
		if cfg.Endpoint != "" {
			endpoint := strings.TrimRight(cfg.Endpoint, "/")
			if cfg.DisableTLS && !strings.Contains(endpoint, "://") {
				endpoint = "http://" + endpoint
			}
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &S3Store{bucket: cfg.Bucket, client: client, presign: s3.NewPresignClient(client)}, nil
}

func (s *S3Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if createErr != nil {
		return fmt.Errorf("ensure bucket %q: head: %v; create: %w", s.bucket, err, createErr)
	}
	return nil
}

func (s *S3Store) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (Info, error) {
	input := &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: body, ContentType: aws.String(contentType)}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	out, err := s.client.PutObject(ctx, input)
	if err != nil {
		return Info{}, fmt.Errorf("put object %q: %w", key, err)
	}
	return Info{Key: key, Size: size, ContentType: contentType, ETag: strings.Trim(aws.ToString(out.ETag), "\"")}, nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, Info{}, mapS3Error(err)
	}
	info := Info{Key: key, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType), ETag: strings.Trim(aws.ToString(out.ETag), "\"")}
	if out.LastModified != nil {
		info.ModifiedAt = *out.LastModified
	}
	return out.Body, info, nil
}

func (s *S3Store) Stat(ctx context.Context, key string) (Info, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return Info{}, mapS3Error(err)
	}
	info := Info{Key: key, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType), ETag: strings.Trim(aws.ToString(out.ETag), "\"")}
	if out.LastModified != nil {
		info.ModifiedAt = *out.LastModified
	}
	return info, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", fmt.Errorf("signed URL TTL must be positive")
	}
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("sign object %q: %w", key, err)
	}
	if _, err := url.ParseRequestURI(out.URL); err != nil {
		return "", fmt.Errorf("object store returned invalid signed URL: %w", err)
	}
	return out.URL, nil
}

func mapS3Error(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound") {
		return ErrNotFound
	}
	return err
}
