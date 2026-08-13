// mirror-copy.go 提供 mirror 的对象复制与批量删除原语.
// 复制分两种路径: 同 endpoint 走服务端 CopyObject (零拷贝); 跨 endpoint 走
// download -> upload (小文件直传, 大文件 Range 分片). 批量删除按 S3 上限分批.

package action

import (
	"context"
	"fmt"
	"io"
	"strings"

	"s3cli/pkg/s3iface"
)

// sameEndpoint 判断源/目标是否同一 endpoint, 是的话可用服务端 CopyObject.
func sameEndpoint(src, tgt *S3PathOptions) bool {
	sc, err1 := src.Client.GetS3Credentials()
	tc, err2 := tgt.Client.GetS3Credentials()
	if err1 != nil || err2 != nil {
		return false
	}
	// 规范化: 去掉尾部斜杠, 避免 "http://s3.example.com" 与 "http://s3.example.com/" 被判为不同
	normalize := func(e string) string { return strings.TrimRight(e, "/") }
	return strings.EqualFold(normalize(sc.BaseEndpoint), normalize(tc.BaseEndpoint))
}

// copyObjectSameEndpoint 同 endpoint 服务端复制.
func copyObjectSameEndpoint(c *Action, srcBucket, srcKey, tgtBucket, tgtKey, storageClass string) error {
	_, err := c.S3.CopyObject(c.Ctx, srcBucket, srcKey, tgtBucket, tgtKey, &s3iface.CopyObjectOptions{
		MetadataDirective: "COPY",
		StorageClass:      storageClass,
	})
	if err != nil {
		return fmt.Errorf("copy: %s", FormatAPIError(err))
	}
	return nil
}

// copyObjectCrossEndpoint 跨 endpoint: download -> upload.
// 自动处理小文件直传和大文件分片.
// report 用于在传输过程中实时上报新增字节 (增量), 可为 nil.
func copyObjectCrossEndpoint(
	src, tgt *Action,
	srcBucket, srcKey, tgtBucket, tgtKey, storageClass string,
	partSize int64,
	report func(n int64),
) error {
	headResp, err := src.S3.HeadObject(src.Ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("head %s: %s", src.S3Path(srcBucket, srcKey), FormatAPIError(err))
	}

	totalSize := headResp.ContentLength
	if totalSize <= partSize {
		return copySingleCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, storageClass)
	}
	return copyMultipartCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, storageClass, totalSize, partSize, headResp, report)
}

// copySingleCrossEndpoint 跨端复制单个 (小) 对象: 整体下载到内存再 PutObject.
// HTTP 响应流不可 seek, 而签名需要可 seek 的 body 计算 payload hash, 故先读入内存.
func copySingleCrossEndpoint(src, tgt *Action, srcBucket, srcKey, tgtBucket, tgtKey, storageClass string) error {
	getResp, err := src.S3.GetObject(src.Ctx, srcBucket, srcKey, nil)
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %s", srcBucket, srcKey, FormatAPIError(err))
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(getResp.Body)

	// PutObject 在签名前需要计算 payload hash, 要求 body 可 seek.
	// HTTP 响应流不可 seek, 故先读入内存再用可 seek 的 bytes.Reader 上传.
	data, err := io.ReadAll(getResp.Body)
	if err != nil {
		return fmt.Errorf("read s3://%s/%s: %v", srcBucket, srcKey, err)
	}

	if _, err := tgt.S3.PutObject(tgt.Ctx, tgtBucket, tgtKey, data, &s3iface.PutObjectOptions{
		ContentType:        getResp.ContentType,
		CacheControl:       getResp.CacheControl,
		ContentDisposition: getResp.ContentDisposition,
		ContentEncoding:    getResp.ContentEncoding,
		ContentLanguage:    getResp.ContentLanguage,
		Metadata:           getResp.Metadata,
		StorageClass:       storageClass,
	}); err != nil {
		return fmt.Errorf("put s3://%s/%s: %s", tgtBucket, tgtKey, FormatAPIError(err))
	}
	return nil
}

// copyMultipartCrossEndpoint 跨端分片复制大对象: 用 Range 逐片下载再 UploadPart,
// 内存占用上限为一个分片. 失败时通过 defer 中止已创建的分片上传.
func copyMultipartCrossEndpoint(
	src, tgt *Action,
	srcBucket, srcKey, tgtBucket, tgtKey, storageClass string,
	totalSize, partSize int64,
	head *s3iface.HeadObjectOutput,
	report func(n int64),
) (err error) {
	// 开工前先校验分片数上限, 避免传完所有 part 后 Complete 才失败 (白传)。
	// partSize 已经 multipartPartSize 钳制 >= 5MB。
	if (totalSize+partSize-1)/partSize > maxMultipartParts {
		return fmt.Errorf("object too large for part size %s: %d parts exceeds %d",
			FormatBytes(partSize), (totalSize+partSize-1)/partSize, maxMultipartParts)
	}
	createResp, err := tgt.S3.CreateMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, &s3iface.PutObjectOptions{
		ContentType:        head.ContentType,
		CacheControl:       head.CacheControl,
		ContentDisposition: head.ContentDisposition,
		ContentEncoding:    head.ContentEncoding,
		ContentLanguage:    head.ContentLanguage,
		Metadata:           head.Metadata,
		StorageClass:       storageClass,
	})
	if err != nil {
		return fmt.Errorf("create mpu s3://%s/%s: %s", tgtBucket, tgtKey, FormatAPIError(err))
	}
	uploadID := createResp.UploadID

	defer func() {
		if err != nil {
			// 清理必须用 WithoutCancel: 跨端分片复制失败/取消 (Ctrl+C) 时
			// tgt.Ctx 可能已被取消, 直接传它会连 AbortMultipartUpload 一起取消,
			// 导致服务端残留分片上传。与 multipart-transfer.go 的做法保持一致。
			_ = tgt.S3.AbortMultipartUpload(context.WithoutCancel(tgt.Ctx), tgtBucket, tgtKey, uploadID)
		}
	}()

	var completed []s3iface.CompletedPart
	partNum := int32(1)
	for offset := int64(0); offset < totalSize; offset += partSize {
		end := offset + partSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		rangeStr := fmt.Sprintf("bytes=%d-%d", offset, end)

		getResp, getErr := src.S3.GetObject(src.Ctx, srcBucket, srcKey, &s3iface.GetObjectOptions{Range: rangeStr})
		if getErr != nil {
			err = fmt.Errorf("get part %d: %s", partNum, FormatAPIError(getErr))
			return err
		}

		// UploadPart 同样需要可 seek 的 body 来计算 payload hash。
		partData, readErr := io.ReadAll(getResp.Body)
		_ = getResp.Body.Close()
		if readErr != nil {
			err = fmt.Errorf("read part %d: %v", partNum, readErr)
			return err
		}

		uploadResp, upErr := tgt.S3.UploadPart(tgt.Ctx, tgtBucket, tgtKey, uploadID, int(partNum), partData)
		if upErr != nil {
			err = fmt.Errorf("upload part %d: %s", partNum, FormatAPIError(upErr))
			return err
		}

		completed = append(completed, s3iface.CompletedPart{
			PartNumber: int(partNum),
			ETag:       uploadResp.ETag,
		})
		if report != nil {
			report(end - offset + 1) // 本分片字节数
		}
		partNum++
	}

	_, err = tgt.S3.CompleteMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, uploadID, completed)
	if err != nil {
		return fmt.Errorf("complete mpu: %s", FormatAPIError(err))
	}
	return nil
}

// =============== 批量删除 ===============

// deleteObjectsBatch 按 S3 单次最多 1000 个对象的上限分批删除 (quiet 模式).
func deleteObjectsBatch(c *Action, bucket string, keys []string) error {
	const batchSize = 1000
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]
		objs := make([]s3iface.ObjectIdentifier, len(batch))
		for j, k := range batch {
			objs[j] = s3iface.ObjectIdentifier{Key: k}
		}
		_, err := c.S3.DeleteObjects(c.Ctx, bucket, objs, true)
		if err != nil {
			return fmt.Errorf("delete batch on s3://%s: %s", bucket, FormatAPIError(err))
		}
	}
	return nil
}
