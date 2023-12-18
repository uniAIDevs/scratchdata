package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"scratchdata/config"
	"scratchdata/pkg/filestore"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// Storage implements filestore.StorageBackend using Amazon S3
type Storage struct {
	ctx        context.Context
	client     *s3.S3
	downloader *s3manager.Downloader
	accessKey  string
	bucket     string
}

// Upload implements filestore.StorageBackend.Upload
func (s *Storage) Upload(path string, r io.ReadSeeker) error {
	// keep track of the file position in-case we need to retry the upload
	rPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("Storage.Upload: Cannnot get current file position: %w", err)
	}

	input := &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(path),
		Body:               r,
		ContentDisposition: aws.String("attachment"),
	}

	_, err = s.client.PutObjectWithContext(s.ctx, input)
	if err == nil {
		return nil
	}
	var awsErr awserr.Error
	if !errors.As(err, &awsErr) || awsErr.Code() != s3.ErrCodeNoSuchBucket {
		return fmt.Errorf("Storage.Upload: %s: %w", path, err)
	}

	// if the bucket doesn't exist, create it and try again
	if _, err := s.client.CreateBucketWithContext(s.ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		return fmt.Errorf("Storage.Upload: %s: Cannot create bucket: %w", path, err)
	}
	// reset the file position
	if _, err := r.Seek(rPos, io.SeekStart); err != nil {
		return fmt.Errorf("Storage.Upload: Cannnot set file position: %w", err)
	}
	if _, err := s.client.PutObjectWithContext(s.ctx, input); err != nil {
		return fmt.Errorf("Storage.Upload: %s: %w", path, err)
	}
	return nil
}

// Download implements filestore.StorageBackend.Download
func (s *Storage) Download(path string, w io.WriterAt) error {
	_, err := s.downloader.DownloadWithContext(s.ctx, w, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	if err == nil {
		return nil
	}
	var awsErr awserr.Error
	if errors.As(err, &awsErr) && awsErr.Code() == s3.ErrCodeNoSuchKey {
		return fmt.Errorf("Storage.Upload: %s: %w", path, filestore.ErrNotFound)
	}
	return fmt.Errorf("Storage.Download: %s: %w", path, err)
}

// NewStorage returns a new initialized Storage
func NewStorage(ctx context.Context, c *config.S3) *Storage {
	awsCreds := credentials.NewStaticCredentials(c.AccessKeyId, c.SecretAccessKey, "")
	awsConfig := aws.NewConfig().
		WithRegion(c.Region).
		WithCredentials(awsCreds)

	if c.Endpoint != "" {
		awsConfig.WithEndpoint(c.Endpoint)
	}

	storageCreds := credentials.NewStaticCredentials(c.AccessKeyId, c.SecretAccessKey, "")
	storageConfig := aws.NewConfig().
		WithRegion(c.Region).
		WithCredentials(storageCreds).
		WithS3ForcePathStyle(true)

	if c.Endpoint != "" {
		storageConfig.WithEndpoint(c.Endpoint)
	}

	client := s3.New(session.Must(session.NewSession()), storageConfig)
	return &Storage{
		ctx:        ctx,
		client:     client,
		downloader: s3manager.NewDownloaderWithClient(client),
		bucket:     c.S3Bucket,
		accessKey:  c.SecretAccessKey,
	}
}
