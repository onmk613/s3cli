// multipart-transfer.go 实现分片上传的底层传输:
//   - uploadMultipart: 已知大小 reader 的分片上传;
//   - uploadUnknownSize: stdin 等未知大小流的分片上传 (先试探首片再决定 PUT/MPU);
//   - uploadMultipartFile: 基于本地状态文件的服务端对账式断点续传.
//
// 任何分片或完成失败都会中止服务端 upload 以释放空间.

package action

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"s3cli/internal/api"
)

const (
	minMultipartPartSize = int64(5 * 1024 * 1024)
	// maxMultipartPartSize 是 S3 单片大小上限 (5GiB)。没有上界时
	// `--part-size 5120` 会直接申请一个 5GiB 的切片并把内存打爆。
	maxMultipartPartSize = int64(5 * 1024 * 1024 * 1024)
	defaultMultipartSize = int64(15 * 1024 * 1024)
	multipartThreshold   = int64(64 * 1024 * 1024)
	maxMultipartParts    = int64(10000)
)

// multipartPartSize 把调用方请求的分片大小 (MB) 钳制到 [5MiB, 5GiB],
// 并在对象很大时自动抬高到能放进 10000 片的最小值。
func multipartPartSize(requestedMB int, totalSize int64) int64 {
	size := defaultMultipartSize
	if requestedMB > 0 {
		size = int64(requestedMB) * 1024 * 1024
	}
	if size < minMultipartPartSize {
		size = minMultipartPartSize
	}
	if size > maxMultipartPartSize {
		size = maxMultipartPartSize
	}
	if totalSize > 0 {
		minimumForPartLimit := (totalSize + maxMultipartParts - 1) / maxMultipartParts
		if size < minimumForPartLimit {
			size = minimumForPartLimit
		}
	}
	return size
}

// uploadMultipart 以固定分片大小上传一个顺序 reader, 分片并发上传
// (见 multipart-parts.go)。失败或取消时中止服务端 upload。
func (c *Action) uploadMultipart(ctx context.Context, bucket, key string, r io.Reader, totalSize int64, partSizeMB int, opts *api.PutObjectOptions, report func(int64)) (err error) {
	alg := opts.ChecksumAlgorithm
	partSize := multipartPartSize(partSizeMB, totalSize)
	create, err := c.S3.CreateMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}
	uploadID := create.UploadID
	defer c.abortOnError(&err, ctx, bucket, key, uploadID)

	// 生产者状态: partNumber 单调递增, done 标记已读到末尾。
	partNumber := 0
	done := false
	readPart := func() (int, []byte, error) {
		if done {
			return 0, nil, io.EOF
		}
		if partNumber >= int(maxMultipartParts) {
			return 0, nil, fmt.Errorf("multipart upload exceeds %d parts", maxMultipartParts)
		}
		// 每片单独分配: 缓冲区一旦交给 worker 就不能复用。
		buf := make([]byte, partSize)
		n, readErr := io.ReadFull(r, buf)
		if n == 0 && (readErr == io.EOF || readErr == io.ErrUnexpectedEOF) {
			done = true
			return 0, nil, io.EOF
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return 0, nil, fmt.Errorf("read multipart part %d: %w", partNumber+1, readErr)
		}
		partNumber++
		if readErr != nil { // EOF / ErrUnexpectedEOF: 本篇是最后一片
			done = true
		}
		return partNumber, buf[:n], nil
	}

	uploadPart := func(ctx context.Context, number int, data []byte) (api.CompletedPart, error) {
		uploaded, uploadErr := c.S3.UploadPartWithChecksum(ctx, bucket, key, uploadID, number, data, alg)
		if uploadErr != nil {
			return api.CompletedPart{}, fmt.Errorf("upload multipart part %d: %w", number, uploadErr)
		}
		part := api.CompletedPart{PartNumber: number, ETag: uploaded.ETag}
		part.SetChecksum(alg, uploaded.Checksum)
		return part, nil
	}

	parts, err := runPartPipeline(ctx, readPart, uploadPart, report)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("multipart upload has no parts")
	}
	if _, err = c.S3.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	return nil
}

// uploadUnknownSize avoids retaining an unbounded stdin stream. Small input
// remains a single PUT; once the first complete part is seen it switches to MPU.
func (c *Action) uploadUnknownSize(ctx context.Context, bucket, key string, r io.Reader, partSizeMB int, opts *api.PutObjectOptions) error {
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
func (c *Action) uploadMultipartFile(ctx context.Context, bucket, key, localPath string, file *os.File, info os.FileInfo, partSizeMB int, opts *api.PutObjectOptions, report func(int64)) error {
	alg := opts.ChecksumAlgorithm
	partSize := multipartPartSize(partSizeMB, info.Size())

	// 内容指纹必须在判定"能否续传"之前算好: size+mtime 相同但内容不同的文件
	// (rsync -t / tar -p / touch -r 还原) 会让续传把旧分片与新内容拼成一个
	// 半新半旧的对象, 且没有任何报错。
	digest, err := fingerprintFile(file, info.Size())
	if err != nil {
		return fmt.Errorf("fingerprint %s: %w", localPath, err)
	}

	// 跨进程互斥: 两个进程同时上传同一 (文件, bucket, key) 会互相覆盖状态
	// 文件并在服务端留下孤儿分片上传, 必须串行化 (见 mpu-lock.go)。
	statePath, err := multipartStatePath(localPath, bucket, key)
	if err != nil {
		return fmt.Errorf("resolve multipart state path: %w", err)
	}
	lock, err := lockMultipartState(statePath)
	if err != nil {
		return fmt.Errorf("lock multipart state: %w", err)
	}
	defer lock.Release()

	state, _, err := loadMultipartState(localPath, bucket, key)
	if err != nil {
		return fmt.Errorf("load multipart state: %w", err)
	}

	var uploadID string
	parts := make([]api.CompletedPart, 0)
	if stateMatches(state, bucket, key, info.Size(), info.ModTime(), digest) && state.PartSize == partSize {
		uploadID = state.UploadID
		listedParts, listErr := c.listAllParts(ctx, bucket, key, uploadID)
		if listErr != nil {
			if !api.HasCode(listErr, "NoSuchUpload") {
				// 保留本地状态文件以便稍后重试 (断点续传不因瞬时故障丢失),
				// 同时提示用户可用 mpu local-clear 丢弃失效的本地状态。
				return fmt.Errorf("list resumable multipart parts: %w (hint: run `s3cli mpu local-clear` to discard the local state if the upload was aborted server-side)", listErr)
			}
			// NoSuchUpload: 服务端该分片上传已不存在 (被 Abort / 过期清理)。
			// 自愈 —— 放弃旧 uploadID, 走下方重建分支重新 CreateMultipartUpload
			// 并保存新状态文件后继续, 而不是让整个上传直接失败。
			uploadID = ""
			parts = nil
		} else {
			for index, part := range listedParts {
				if part.PartNumber != index+1 {
					// 分片号不连续: 旧上传无法安全续传。放弃前先 Abort,
					// 否则服务端会残留孤儿分片上传 (持续占用存储直到生命周期清理)。
					_ = c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, uploadID)
					uploadID = ""
					parts = nil
					break
				}
				// 服务端返回的分片校验和必须带回 Complete 请求: 创建上传时
				// 声明了算法的服务端 (AWS) 会校验每个 CompletedPart 的校验和,
				// 丢失它会导致 Complete 被拒绝或绕过整片校验。
				resumed := api.CompletedPart{PartNumber: part.PartNumber, ETag: part.ETag}
				resumed.SetChecksum(alg, part.ChecksumValues())
				parts = append(parts, resumed)
			}
		}
	} else if state != nil && state.UploadID != "" {
		// 旧状态无法复用: 换了 --part-size (分片边界不兼容), 或指纹/大小/mtime
		// 不再匹配 (文件被改动或替换)。无论哪种, 都要先 Abort 再重建 ——
		// 否则服务端会残留一个再也不会被引用的孤儿分片上传, 一直占存储直到
		// 生命周期规则清理。此前 fingerprint 不匹配时 state 被置 nil, 这条
		// Abort 分支根本不会执行, 孤儿分片必然泄漏。
		_ = c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, state.UploadID)
	}
	if uploadID == "" {
		created, createErr := c.S3.CreateMultipartUpload(ctx, bucket, key, opts)
		if createErr != nil {
			return fmt.Errorf("create multipart upload: %w", createErr)
		}
		uploadID = created.UploadID
		state = &multipartState{Version: 1, UploadID: uploadID, Bucket: bucket, Key: key, LocalPath: localPath, PartSize: partSize, TotalSize: info.Size(), ModTimeUnixNs: info.ModTime().UnixNano(), ContentDigest: digest, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
		if saveErr := saveMultipartState(statePath, *state); saveErr != nil {
			_ = c.S3.AbortMultipartUpload(context.WithoutCancel(ctx), bucket, key, uploadID)
			return fmt.Errorf("save multipart state: %w", saveErr)
		}
	}

	// 从第 len(parts)+1 片继续, 生产者顺序读, worker 并发上传。
	// 已存在的分片 (服务端 ListParts 对账结果) 直接并入待完成列表。
	nextNumber := len(parts) + 1
	nextOffset := int64(len(parts)) * partSize
	if _, err := file.Seek(nextOffset, io.SeekStart); err != nil {
		return fmt.Errorf("seek resumable multipart upload: %w", err)
	}
	total := info.Size()
	done := false
	readPart := func() (int, []byte, error) {
		if done || nextOffset >= total {
			return 0, nil, io.EOF
		}
		if nextNumber > int(maxMultipartParts) {
			return 0, nil, fmt.Errorf("multipart upload exceeds %d parts", maxMultipartParts)
		}
		want := partSize
		if remaining := total - nextOffset; remaining < want {
			want = remaining
		}
		buf := make([]byte, want)
		n, readErr := io.ReadFull(file, buf)
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return 0, nil, fmt.Errorf("read multipart part %d: %w", nextNumber, readErr)
		}
		if int64(n) != want {
			return 0, nil, fmt.Errorf("read multipart part %d: expected %d bytes, got %d", nextNumber, want, n)
		}
		number := nextNumber
		nextNumber++
		nextOffset += int64(n)
		return number, buf[:n], nil
	}
	uploadPart := func(ctx context.Context, number int, data []byte) (api.CompletedPart, error) {
		uploaded, uploadErr := c.S3.UploadPartWithChecksum(ctx, bucket, key, uploadID, number, data, alg)
		if uploadErr != nil {
			return api.CompletedPart{}, fmt.Errorf("upload multipart part %d: %w", number, uploadErr)
		}
		part := api.CompletedPart{PartNumber: number, ETag: uploaded.ETag}
		part.SetChecksum(alg, uploaded.Checksum)
		return part, nil
	}

	uploaded, err := runPartPipeline(ctx, readPart, uploadPart, report)
	if err != nil {
		return err
	}
	parts = append(parts, uploaded...)
	if len(parts) == 0 {
		return fmt.Errorf("multipart upload has no parts")
	}
	if _, err := c.S3.CompleteMultipartUpload(ctx, bucket, key, uploadID, parts); err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed multipart state: %w", err)
	}
	return nil
}
