// mirror-copy.go 提供 mirror 的对象复制与批量删除原语.
// 复制分两种路径: 同 endpoint 走服务端 CopyObject (零拷贝); 跨 endpoint 走
// download -> upload (小文件直传, 大文件 Range 分片). 批量删除按 S3 上限分批.

package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
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

// maxCopyObjectSize 是单次 CopyObject 的 AWS 协议上限 (5GiB);
// 更大的源对象会得到 EntityTooLarge, 必须改走 UploadPartCopy 分片复制。
const maxCopyObjectSize = 5 * 1024 * 1024 * 1024

// copyObjectSameEndpoint 在同一 endpoint 内做一次服务端复制 (零下载)。
//
// opts 为可选的覆盖项; nil 表示完全沿用源对象。
//
// 先直接 CopyObject, 只有服务端明确报告 EntityTooLarge 时才 HEAD 源对象并改走
// UploadPartCopy 分片复制 —— 单次 CopyObject 对超过 5GiB 的源会失败。这样常规
// 对象不必为一次大小判断多付一个 HEAD 往返。
//
// mirror、cp、mv 共用这一条路径: 此前 cp/mv 直接调 CopyObject, 导致同一个
// 大对象 mirror 能拷、cp 却必然失败。
func (c *Action) copyObjectSameEndpoint(srcBucket, srcKey, tgtBucket, tgtKey string, opts *api.CopyObjectOptions) error {
	if opts == nil {
		opts = &api.CopyObjectOptions{}
	}

	_, err := c.S3.CopyObject(c.Ctx, srcBucket, srcKey, tgtBucket, tgtKey, opts)
	if err == nil {
		return nil
	}
	if !isEntityTooLarge(err) {
		return fmt.Errorf("copy %s: %w", c.S3Path(tgtBucket, tgtKey), err)
	}

	head, headErr := c.S3.HeadObject(c.Ctx, srcBucket, srcKey, "")
	if headErr != nil {
		return fmt.Errorf("head %s: %w", c.S3Path(srcBucket, srcKey), headErr)
	}
	return c.copyMultipartSameEndpoint(srcBucket, srcKey, tgtBucket, tgtKey, head, opts)
}

// isEntityTooLarge 判断错误是否为"源对象超过单次 CopyObject 的 5GiB 上限"。
// AWS 返回 EntityTooLarge / 413; 兼容实现可能只给 400 + EntityTooLarge。
func isEntityTooLarge(err error) bool {
	var apiErr *api.ErrorResponse
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusRequestEntityTooLarge ||
		strings.Contains(apiErr.Code, "EntityTooLarge") ||
		strings.Contains(apiErr.Code, "TooLarge")
}

// copyMultipartSameEndpoint 同 endpoint 大对象分片复制: 每片用 UploadPartCopy
// 从源对象按 Range 截取, 全程服务端完成、零下载。失败/取消时 Abort 已创建的上传。
//
// 分片复制的元数据/标签只能在 CreateMultipartUpload 阶段指定, 因此这里把
// opts 中的 Metadata / Tagging / StorageClass 透传给初始化请求。
func (c *Action) copyMultipartSameEndpoint(srcBucket, srcKey, tgtBucket, tgtKey string, head *api.HeadObjectOutput, opts *api.CopyObjectOptions) (err error) {
	putOpts := &api.PutObjectOptions{
		ContentType:        head.ContentType,
		ContentEncoding:    head.ContentEncoding,
		ContentDisposition: head.ContentDisposition,
		ContentLanguage:    head.ContentLanguage,
		CacheControl:       head.CacheControl,
		StorageClass:       opts.StorageClass,
	}
	if opts.MetadataDirective == "REPLACE" {
		putOpts.Metadata = opts.Metadata
	} else {
		putOpts.Metadata = head.Metadata
	}
	if opts.TaggingDirective == "REPLACE" {
		putOpts.Tagging = opts.Tagging
	} else {
		// 未要求替换标签时沿用源对象标签 (GetObjectTagging 的 URL 形式)。
		if tags, tagErr := c.S3.GetObjectTagging(c.Ctx, srcBucket, srcKey, ""); tagErr == nil && len(tags) > 0 {
			putOpts.Tagging = api.EncodeTagging(tags)
		}
	}

	create, err := c.S3.CreateMultipartUpload(c.Ctx, tgtBucket, tgtKey, putOpts)
	if err != nil {
		return fmt.Errorf("create mpu %s: %w", c.S3Path(tgtBucket, tgtKey), err)
	}
	uploadID := create.UploadID
	defer func() {
		if err != nil {
			// 清理用 WithoutCancel: 失败/取消 (Ctrl+C) 时 c.Ctx 可能已被取消,
			// 直接传它会连 Abort 一起取消, 服务端残留分片。
			_ = c.S3.AbortMultipartUpload(context.WithoutCancel(c.Ctx), tgtBucket, tgtKey, uploadID)
		}
	}()

	// 分片大小取协议上限 (5GiB): UploadPartCopy 是纯服务端操作, 分片越大
	// 请求数越少。10000 片上限对应 50000 GiB, 实际不可达, 保留判断只为兜底。
	partSize := int64(maxCopyObjectSize)
	var completed []api.CompletedPart
	for partNum, offset := 1, int64(0); offset < head.ContentLength; partNum, offset = partNum+1, offset+partSize {
		if partNum > int(maxMultipartParts) {
			err = fmt.Errorf("copy %s: exceeds %d parts, increase per-part coverage", c.S3Path(tgtBucket, tgtKey), maxMultipartParts)
			return err
		}
		end := offset + partSize - 1
		if end >= head.ContentLength {
			end = head.ContentLength - 1
		}
		part, cpErr := c.S3.UploadPartCopy(c.Ctx, srcBucket, srcKey, tgtBucket, tgtKey, uploadID, partNum,
			&api.UploadPartCopyOptions{CopySourceRange: fmt.Sprintf("bytes=%d-%d", offset, end)})
		if cpErr != nil {
			err = fmt.Errorf("copy part %d: %w", partNum, cpErr)
			return err
		}
		completed = append(completed, api.CompletedPart{PartNumber: partNum, ETag: part.ETag})
	}
	if len(completed) == 0 {
		err = fmt.Errorf("copy %s: source object is empty", c.S3Path(tgtBucket, tgtKey))
		return err
	}
	if _, err = c.S3.CompleteMultipartUpload(c.Ctx, tgtBucket, tgtKey, uploadID, completed); err != nil {
		return fmt.Errorf("complete mpu %s: %w", c.S3Path(tgtBucket, tgtKey), err)
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
		return fmt.Errorf("head %s: %w", src.S3Path(srcBucket, srcKey), err)
	}

	totalSize := headResp.ContentLength
	if totalSize <= partSize {
		return copySingleCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, storageClass, headResp)
	}
	return copyMultipartCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, storageClass, totalSize, partSize, headResp, report)
}

// copySingleCrossEndpoint 跨端复制单个 (小) 对象: 下载到临时文件再流式
// PutObjectStream 上传。HTTP 响应流不可 seek 而签名需要可 seek 的 body,
// 旧实现因此把整个对象读入内存 —— part-size 调大时小对象阈值随之放大,
// 并发复制多个对象会把内存推到 partSize × 并发。落盘临时文件后内存与
// 对象大小无关 (TMPDIR 可指到大容量磁盘)。
func copySingleCrossEndpoint(src, tgt *Action, srcBucket, srcKey, tgtBucket, tgtKey, storageClass string, head *api.HeadObjectOutput) error {
	getResp, err := src.S3.GetObject(src.Ctx, srcBucket, srcKey, nil)
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %w", srcBucket, srcKey, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(getResp.Body)

	tmp, err := os.CreateTemp("", "s3cli-mirror-obj-*")
	if err != nil {
		return fmt.Errorf("create spool file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	n, err := io.Copy(tmp, getResp.Body)
	if err != nil {
		// 用 %w 保留错误链: 读取失败可能是传输层的 *api.ErrorResponse,
		// 断链会让退出码退化为 1, 而不是 4/5。
		return fmt.Errorf("read s3://%s/%s: %w", srcBucket, srcKey, err)
	}
	// 截断 / 源对象在 HEAD 之后被改写: 宁可失败也不能上传一个坏对象。
	if n != head.ContentLength {
		return fmt.Errorf("read s3://%s/%s: got %d bytes, expected %d (source changed or connection truncated)", srcBucket, srcKey, n, head.ContentLength)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind spool file: %w", err)
	}

	if _, err := tgt.S3.PutObjectStream(tgt.Ctx, tgtBucket, tgtKey, tmp, n, &api.PutObjectOptions{
		ContentType:        getResp.ContentType,
		CacheControl:       getResp.CacheControl,
		ContentDisposition: getResp.ContentDisposition,
		ContentEncoding:    getResp.ContentEncoding,
		ContentLanguage:    getResp.ContentLanguage,
		Metadata:           getResp.Metadata,
		StorageClass:       storageClass,
	}); err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", tgtBucket, tgtKey, err)
	}
	return nil
}

// parseContentRange 解析 "bytes start-end/total" 形式的 Content-Range 头,
// total 为 "*" (未知总长) 时返回 total=-1。
func parseContentRange(cr string) (start, end, total int64, ok bool) {
	rest, found := strings.CutPrefix(cr, "bytes ")
	if !found {
		return 0, 0, 0, false
	}
	rangePart, totalPart, _ := strings.Cut(rest, "/")
	startStr, endStr, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startStr), 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	end, err = strconv.ParseInt(strings.TrimSpace(endStr), 10, 64)
	if err != nil || end < start || start < 0 {
		return 0, 0, 0, false
	}
	if totalPart == "*" {
		return start, end, -1, true
	}
	total, err = strconv.ParseInt(strings.TrimSpace(totalPart), 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

// verifyRangePart 校验 Range GET 的响应确实是请求的那一段。
//
// 只看长度不够: 个别网关会忽略 Range 头直接返回 200 全量对象, 旧实现会把
// 全量字节当分片数据写入, 分片错位、最终对象静默损坏。必须核对
// Content-Range 的起止与请求一致 (200 全量响应没有 Content-Range, 立刻可辨)。
func verifyRangePart(contentRange string, wantStart, wantEnd, totalSize int64, partNum int) error {
	if contentRange == "" {
		return fmt.Errorf("get part %d: server ignored the Range request (no Content-Range header); refusing to assemble a corrupted object", partNum)
	}
	start, end, total, ok := parseContentRange(contentRange)
	if !ok {
		return fmt.Errorf("get part %d: malformed Content-Range %q", partNum, contentRange)
	}
	if start != wantStart || end != wantEnd {
		return fmt.Errorf("get part %d: server returned range %d-%d, requested %d-%d", partNum, start, end, wantStart, wantEnd)
	}
	if total >= 0 && total != totalSize {
		return fmt.Errorf("get part %d: source object size changed during copy (Content-Range total %d, expected %d)", partNum, total, totalSize)
	}
	return nil
}

// copyCrossEndpointPart 下载并上传跨端复制的一个分片:
// Range GET -> 校验 Content-Range -> 落盘临时文件 -> UploadPartReader 流式上传。
// 返回已完成的分片描述; 临时文件随函数返回删除, 内存占用与分片大小无关。
func copyCrossEndpointPart(
	ctx context.Context,
	src, tgt *Action,
	srcBucket, srcKey, tgtBucket, tgtKey, uploadID string,
	partNum int, offset, end, totalSize int64,
) (api.CompletedPart, error) {
	rangeStr := fmt.Sprintf("bytes=%d-%d", offset, end)
	getResp, err := src.S3.GetObject(ctx, srcBucket, srcKey, &api.GetObjectOptions{Range: rangeStr})
	if err != nil {
		return api.CompletedPart{}, fmt.Errorf("get part %d: %w", partNum, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(getResp.Body)

	if err := verifyRangePart(getResp.ContentRange, offset, end, totalSize, partNum); err != nil {
		return api.CompletedPart{}, err
	}
	wantLen := end - offset + 1
	if getResp.ContentLength > 0 && getResp.ContentLength != wantLen {
		return api.CompletedPart{}, fmt.Errorf("get part %d: Content-Length %d, expected %d", partNum, getResp.ContentLength, wantLen)
	}

	tmp, err := os.CreateTemp("", fmt.Sprintf("s3cli-mirror-part-%d-*", partNum))
	if err != nil {
		return api.CompletedPart{}, fmt.Errorf("create spool file: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	n, err := io.Copy(tmp, getResp.Body)
	if err != nil {
		return api.CompletedPart{}, fmt.Errorf("read part %d: %w", partNum, err)
	}
	if n != wantLen {
		return api.CompletedPart{}, fmt.Errorf("read part %d: got %d bytes, expected %d", partNum, n, wantLen)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return api.CompletedPart{}, fmt.Errorf("rewind spool file: %w", err)
	}

	uploadResp, err := tgt.S3.UploadPartReader(ctx, tgtBucket, tgtKey, uploadID, partNum, tmp, n, "")
	if err != nil {
		return api.CompletedPart{}, fmt.Errorf("upload part %d: %w", partNum, err)
	}
	return api.CompletedPart{PartNumber: partNum, ETag: uploadResp.ETag}, nil
}

// copyMultipartCrossEndpoint 跨端分片复制大对象: 分片并行处理, 每片
// "Range 下载 -> 落盘临时文件 -> 流式上传"。内存占用与 part-size 无关
// (旧实现逐片 io.ReadAll 且串行, --part-size 5120 时小对象阈值内的对象
// 也会整块驻留内存, 吞吐只有一条连接)。失败时通过 defer 中止已创建的分片上传。
func copyMultipartCrossEndpoint(
	src, tgt *Action,
	srcBucket, srcKey, tgtBucket, tgtKey, storageClass string,
	totalSize, partSize int64,
	head *api.HeadObjectOutput,
	report func(n int64),
) (err error) {
	// 开工前先校验分片数上限, 避免传完所有 part 后 Complete 才失败 (白传)。
	// partSize 已经 multipartPartSize 钳制 >= 5MB。
	partCount := (totalSize + partSize - 1) / partSize
	if partCount > maxMultipartParts {
		return fmt.Errorf("object too large for part size %s: %d parts exceeds %d",
			myprint.FormatBytes(partSize), partCount, maxMultipartParts)
	}
	createResp, err := tgt.S3.CreateMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, &api.PutObjectOptions{
		ContentType:        head.ContentType,
		CacheControl:       head.CacheControl,
		ContentDisposition: head.ContentDisposition,
		ContentEncoding:    head.ContentEncoding,
		ContentLanguage:    head.ContentLanguage,
		Metadata:           head.Metadata,
		StorageClass:       storageClass,
	})
	if err != nil {
		return fmt.Errorf("create mpu s3://%s/%s: %w", tgtBucket, tgtKey, err)
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

	// 任一分片失败即取消其余分片: 半个对象没有意义, 尽早收敛少浪费流量。
	pipeCtx, cancel := context.WithCancel(tgt.Ctx)
	defer cancel()

	// 对象内分片并发与本地上传流水线取同一并发度; 对象间并发由 mirror 的
	// 信号量控制, 两者相乘即为连接总数上限。
	workers := min(int64(multipartConcurrency), partCount)
	sem := make(chan struct{}, workers)
	results := make(chan api.CompletedPart, partCount)
	fail := make(chan error, 1)

	var wg sync.WaitGroup
	for partNum := int64(1); partNum <= partCount; partNum++ {
		offset := (partNum - 1) * partSize
		end := offset + partSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(partNum, offset, end int64) {
			defer wg.Done()
			defer func() { <-sem }()
			if pipeCtx.Err() != nil {
				return
			}
			part, partErr := copyCrossEndpointPart(pipeCtx, src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, uploadID, int(partNum), offset, end, totalSize)
			if partErr != nil {
				select {
				case fail <- partErr:
				default:
				}
				cancel()
				return
			}
			results <- part
			if report != nil {
				report(end - offset + 1) // 本分片字节数
			}
		}(partNum, offset, end)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var completed []api.CompletedPart
	for part := range results {
		completed = append(completed, part)
	}
	select {
	case ferr := <-fail:
		err = ferr
		return err
	default:
	}
	if int64(len(completed)) != partCount {
		err = fmt.Errorf("copy %s: only %d of %d parts completed", tgt.S3Path(tgtBucket, tgtKey), len(completed), partCount)
		return err
	}
	sort.Slice(completed, func(i, j int) bool { return completed[i].PartNumber < completed[j].PartNumber })

	_, err = tgt.S3.CompleteMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, uploadID, completed)
	if err != nil {
		return fmt.Errorf("complete mpu: %w", err)
	}
	return nil
}

// =============== 批量删除 ===============
