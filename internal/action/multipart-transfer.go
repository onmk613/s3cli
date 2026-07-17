package action

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"s3cli/pkg/s3api"
)

const (
	minMultipartPartSize = int64(5 * 1024 * 1024)
	defaultMultipartSize = int64(15 * 1024 * 1024)
	multipartThreshold   = int64(64 * 1024 * 1024)
	maxMultipartParts    = int64(10000)
)

func multipartPartSize(requestedMB int, totalSize int64) int64 {
	size := defaultMultipartSize
	if requestedMB > 0 {
		size = int64(requestedMB) * 1024 * 1024
	}
	if size < minMultipartPartSize {
		size = minMultipartPartSize
	}
	if totalSize > 0 {
		minimumForPartLimit := (totalSize + maxMultipartParts - 1) / maxMultipartParts
		if size < minimumForPartLimit {
			size = minimumForPartLimit
		}
	}
	return size
}

// uploadMultipart streams fixed-size parts, bounds memory to one part, and
// aborts the server-side upload whenever a part or completion fails.
func (c *S3Client) uploadMultipart(ctx context.Context, bucket, key string, r io.Reader, totalSize int64, partSizeMB int, opts *s3api.PutObjectOptions, report func(int64)) (err error) {
	partSize := multipartPartSize(partSizeMB, totalSize)
	create, err := c.S3.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}
	defer func() {
		if err != nil {
			// Cleanup must not be skipped merely because the transfer context was cancelled.
			_ = c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, create.UploadID)
		}
	}()

	parts := make([]s3api.CompletedPart, 0)
	buf := make([]byte, partSize)
	for partNumber := 1; ; partNumber++ {
		if partNumber > int(maxMultipartParts) {
			return fmt.Errorf("multipart upload exceeds %d parts", maxMultipartParts)
		}
		n, readErr := io.ReadFull(r, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return fmt.Errorf("read multipart part %d: %w", partNumber, readErr)
		}
		if n == 0 && readErr == io.EOF {
			break
		}
		uploaded, uploadErr := c.S3.UploadPart(ctx, bucket, key, create.UploadID, partNumber, buf[:n])
		if uploadErr != nil {
			return fmt.Errorf("upload multipart part %d: %w", partNumber, uploadErr)
		}
		parts = append(parts, s3api.CompletedPart{PartNumber: partNumber, ETag: uploaded.ETag})
		if report != nil {
			report(int64(n))
		}
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	if len(parts) == 0 {
		return fmt.Errorf("multipart upload has no parts")
	}
	if _, err = c.S3.CompleteMultipartUpload(ctx, bucket, key, create.UploadID, parts); err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	return nil
}

// uploadUnknownSize avoids retaining an unbounded stdin stream. Small input
// remains a single PUT; once the first complete part is seen it switches to MPU.
func (c *S3Client) uploadUnknownSize(ctx context.Context, bucket, key string, r io.Reader, partSizeMB int, opts *s3api.PutObjectOptions) error {
	partSize := multipartPartSize(partSizeMB, 0)
	first := make([]byte, partSize)
	n, err := io.ReadFull(r, first)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		_, putErr := c.S3.PutObject(ctx, bucket, key, first[:n], opts)
		return putErr
	}
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return c.uploadMultipart(ctx, bucket, key, io.MultiReader(bytes.NewReader(first), r), 0, partSizeMB, opts, nil)
}

// uploadMultipartFile resumes a matching local-file upload when possible. The
// server's ListParts response is authoritative, so a stale or edited local
// state file can never cause unverified parts to be completed.
func (c *S3Client) uploadMultipartFile(ctx context.Context, bucket, key, localPath string, file *os.File, info os.FileInfo, partSizeMB int, opts *s3api.PutObjectOptions, report func(int64)) error {
	partSize := multipartPartSize(partSizeMB, info.Size())
	state, statePath, err := loadMultipartState(localPath, bucket, key, info.Size(), info.ModTime())
	if err != nil {
		return fmt.Errorf("load multipart state: %w", err)
	}

	var uploadID string
	parts := make([]s3api.CompletedPart, 0)
	if state != nil && state.PartSize == partSize {
		uploadID = state.UploadID
		listed, listErr := c.S3.ListParts(ctx, bucket, key, uploadID, 0, int(maxMultipartParts))
		if listErr == nil {
			for index, part := range listed.Parts {
				if part.PartNumber != index+1 {
					uploadID = ""
					parts = nil
					break
				}
				parts = append(parts, s3api.CompletedPart{PartNumber: part.PartNumber, ETag: part.ETag})
			}
		} else {
			// Preserve the state so a transient ListParts failure remains resumable.
			return fmt.Errorf("list resumable multipart parts: %w", listErr)
		}
	}
	if uploadID == "" {
		created, createErr := c.S3.CreateMultipartUpload(ctx, bucket, key, opts)
		if createErr != nil {
			return fmt.Errorf("create multipart upload: %w", createErr)
		}
		uploadID = created.UploadID
		state = &multipartState{Version: 1, UploadID: uploadID, Bucket: bucket, Key: key, LocalPath: localPath, PartSize: partSize, TotalSize: info.Size(), ModTimeUnixNs: info.ModTime().UnixNano(), CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		if saveErr := saveMultipartState(statePath, *state); saveErr != nil {
			_ = c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, uploadID)
			return fmt.Errorf("save multipart state: %w", saveErr)
		}
	}

	offset := int64(len(parts)) * partSize
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek resumable multipart upload: %w", err)
	}
	buf := make([]byte, partSize)
	for partNumber := len(parts) + 1; offset < info.Size(); partNumber++ {
		if partNumber > int(maxMultipartParts) {
			return fmt.Errorf("multipart upload exceeds %d parts", maxMultipartParts)
		}
		remaining := info.Size() - offset
		want := partSize
		if remaining < want {
			want = remaining
		}
		n, readErr := io.ReadFull(file, buf[:want])
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("read multipart part %d: %w", partNumber, readErr)
		}
		if int64(n) != want {
			return fmt.Errorf("read multipart part %d: expected %d bytes, got %d", partNumber, want, n)
		}
		uploaded, uploadErr := c.S3.UploadPart(ctx, bucket, key, uploadID, partNumber, buf[:n])
		if uploadErr != nil {
			return fmt.Errorf("upload multipart part %d: %w", partNumber, uploadErr)
		}
		parts = append(parts, s3api.CompletedPart{PartNumber: partNumber, ETag: uploaded.ETag})
		offset += int64(n)
		if report != nil {
			report(int64(n))
		}
	}
	if _, err := c.S3.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed multipart state: %w", err)
	}
	return nil
}
