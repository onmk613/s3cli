package action

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/progress"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// =============== 配置 ===============

// S3PathOptions 描述一个 S3 端点 + bucket + key 前缀。
//
// 这里持有的是 *S3Client（而非裸 *s3.Client），与单端 action（copy/mv/...）
// 完全一致：天然携带 Ctx / Alias / GetCreds() 等上下文，便于双端统一处理。
type S3PathOptions struct {
	Client        *S3Client
	Bucket        string
	ObjectKey     string // 作为前缀；可为空表示整个 bucket
	TrailingSlash bool   // 输入是否以 "/" 结尾（语义上是“目录”）
}

// MirrorOptions Mirror 主入口参数。
type MirrorOptions struct {
	Src *S3PathOptions
	Tgt *S3PathOptions

	Remove      bool  // 是否删除目标端多余的对象
	Overwrite   bool  // 已存在时是否依据 ETag/Size 覆盖
	DryRun      bool  // 仅打印将要做的事，不实际执行
	Concurrency int   // 并发数 (默认 8)
	PartSizeMB  int   // 分片大小 MB (默认 64)
	SizeLimit   int64 // 单对象大小上限 (字节)，0 表示不限制
}

// =============== 对象信息 ===============

type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// listAllObjects 列出 bucket 下 prefix 的所有对象。
func listAllObjects(c *S3Client, bucket, prefix string) (map[string]ObjectInfo, error) {
	objects := make(map[string]ObjectInfo)

	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %s", bucket, prefix, FormatAPIError(err))
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			objects[key] = ObjectInfo{
				Key:          key,
				Size:         aws.ToInt64(obj.Size),
				ETag:         strings.Trim(aws.ToString(obj.ETag), `"`),
				LastModified: aws.ToTime(obj.LastModified),
			}
		}
	}
	return objects, nil
}

// =============== 差异计算 ===============

type diffResult struct {
	ToCopy   []string // 需要 src -> tgt 的 key（相对路径）
	ToDelete []string // 需要从目标删除的 key（相对路径）
}

// calcDiff 计算 src->tgt 的差异。入参的 key 是“相对路径”，便于跨前缀比较。
func calcDiff(source, target map[string]ObjectInfo, overwrite bool) diffResult {
	var r diffResult
	for key, src := range source {
		tgt, ok := target[key]
		if !ok {
			r.ToCopy = append(r.ToCopy, key)
			continue
		}
		if overwrite && needsUpdate(src, tgt) {
			r.ToCopy = append(r.ToCopy, key)
		}
	}
	for key := range target {
		if _, ok := source[key]; !ok {
			r.ToDelete = append(r.ToDelete, key)
		}
	}
	return r
}

func needsUpdate(src, tgt ObjectInfo) bool {
	// 优先比 ETag（已去引号）。MPU 上传时 ETag 形如 "xxx-N"，两端不可比，
	// 此时退化到 size + last-modified。
	if !strings.Contains(src.ETag, "-") && !strings.Contains(tgt.ETag, "-") &&
		src.ETag != "" && tgt.ETag != "" {
		return src.ETag != tgt.ETag
	}
	if src.Size != tgt.Size {
		return true
	}
	return src.LastModified.After(tgt.LastModified)
}

// =============== 复制对象 ===============

// sameEndpoint 判断源/目标是否同一 endpoint，是的话可用服务端 CopyObject。
func sameEndpoint(src, tgt *S3PathOptions) bool {
	sc, err1 := src.Client.GetCreds()
	tc, err2 := tgt.Client.GetCreds()
	if err1 != nil || err2 != nil {
		return false
	}
	// 规范化：去掉尾部斜杠，避免 "http://s3.example.com" 与 "http://s3.example.com/" 被判为不同
	normalize := func(e string) string { return strings.TrimRight(e, "/") }
	return strings.EqualFold(normalize(sc.BaseEndpoint), normalize(tc.BaseEndpoint))
}

// copyObjectSameEndpoint 同 endpoint 服务端复制。
func copyObjectSameEndpoint(c *S3Client, srcBucket, srcKey, tgtBucket, tgtKey string) error {
	_, err := c.S3.CopyObject(c.Ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(tgtBucket),
		Key:               aws.String(tgtKey),
		CopySource:        aws.String(srcBucket + "/" + srcKey),
		MetadataDirective: s3types.MetadataDirectiveCopy,
	})
	if err != nil {
		return fmt.Errorf("copy: %s", FormatAPIError(err))
	}
	return nil
}

// copyObjectCrossEndpoint 跨 endpoint：download -> upload。
// 自动处理小文件直传和大文件分片。
func copyObjectCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
	partSize int64,
) error {
	headResp, err := src.S3.HeadObject(src.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("head s3://%s/%s: %s", srcBucket, srcKey, FormatAPIError(err))
	}

	totalSize := aws.ToInt64(headResp.ContentLength)
	if totalSize <= partSize {
		return copySingleCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey)
	}
	return copyMultipartCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, totalSize, partSize, headResp)
}

func copySingleCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
) error {
	getResp, err := src.S3.GetObject(src.Ctx, &s3.GetObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %s", srcBucket, srcKey, FormatAPIError(err))
	}
	defer getResp.Body.Close()

	put := &s3.PutObjectInput{
		Bucket:             aws.String(tgtBucket),
		Key:                aws.String(tgtKey),
		Body:               getResp.Body,
		ContentLength:      getResp.ContentLength,
		ContentType:        getResp.ContentType,
		CacheControl:       getResp.CacheControl,
		ContentDisposition: getResp.ContentDisposition,
		ContentEncoding:    getResp.ContentEncoding,
		ContentLanguage:    getResp.ContentLanguage,
		Metadata:           getResp.Metadata,
	}
	if _, err := tgt.S3.PutObject(tgt.Ctx, put); err != nil {
		return fmt.Errorf("put s3://%s/%s: %s", tgtBucket, tgtKey, FormatAPIError(err))
	}
	return nil
}

func copyMultipartCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
	totalSize, partSize int64,
	head *s3.HeadObjectOutput,
) (err error) {
	createResp, err := tgt.S3.CreateMultipartUpload(tgt.Ctx, &s3.CreateMultipartUploadInput{
		Bucket:             aws.String(tgtBucket),
		Key:                aws.String(tgtKey),
		ContentType:        head.ContentType,
		CacheControl:       head.CacheControl,
		ContentDisposition: head.ContentDisposition,
		ContentEncoding:    head.ContentEncoding,
		ContentLanguage:    head.ContentLanguage,
		Metadata:           head.Metadata,
	})
	if err != nil {
		return fmt.Errorf("create mpu s3://%s/%s: %s", tgtBucket, tgtKey, FormatAPIError(err))
	}
	uploadID := aws.ToString(createResp.UploadId)

	defer func() {
		if err != nil {
			_, _ = tgt.S3.AbortMultipartUpload(tgt.Ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(tgtBucket),
				Key:      aws.String(tgtKey),
				UploadId: aws.String(uploadID),
			})
		}
	}()

	var completed []s3types.CompletedPart
	partNum := int32(1)
	for offset := int64(0); offset < totalSize; offset += partSize {
		end := offset + partSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		rangeStr := fmt.Sprintf("bytes=%d-%d", offset, end)

		getResp, getErr := src.S3.GetObject(src.Ctx, &s3.GetObjectInput{
			Bucket: aws.String(srcBucket),
			Key:    aws.String(srcKey),
			Range:  aws.String(rangeStr),
		})
		if getErr != nil {
			err = fmt.Errorf("get part %d: %s", partNum, FormatAPIError(getErr))
			return err
		}

		uploadResp, upErr := tgt.S3.UploadPart(tgt.Ctx, &s3.UploadPartInput{
			Bucket:        aws.String(tgtBucket),
			Key:           aws.String(tgtKey),
			UploadId:      aws.String(uploadID),
			PartNumber:    aws.Int32(partNum),
			Body:          getResp.Body,
			ContentLength: getResp.ContentLength,
		})
		getResp.Body.Close()
		if upErr != nil {
			err = fmt.Errorf("upload part %d: %s", partNum, FormatAPIError(upErr))
			return err
		}

		completed = append(completed, s3types.CompletedPart{
			PartNumber: aws.Int32(partNum),
			ETag:       uploadResp.ETag,
		})
		partNum++
	}

	_, err = tgt.S3.CompleteMultipartUpload(tgt.Ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(tgtBucket),
		Key:      aws.String(tgtKey),
		UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: completed,
		},
	})
	if err != nil {
		return fmt.Errorf("complete mpu: %s", FormatAPIError(err))
	}
	return nil
}

// =============== 批量删除 ===============

func deleteObjectsBatch(c *S3Client, bucket string, keys []string) error {
	const batchSize = 1000
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]
		objs := make([]s3types.ObjectIdentifier, len(batch))
		for j, k := range batch {
			objs[j] = s3types.ObjectIdentifier{Key: aws.String(k)}
		}
		_, err := c.S3.DeleteObjects(c.Ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{
				Objects: objs,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("delete batch on s3://%s: %s", bucket, FormatAPIError(err))
		}
	}
	return nil
}

// =============== key 重映射 ===============

// relKey 把绝对 key 转为相对源前缀的路径。
//
//	prefix="a/b/", key="a/b/c/d.txt" -> "c/d.txt"
//	prefix="",     key="x/y.txt"     -> "x/y.txt"
func relKey(key, prefix string) string {
	if prefix == "" {
		return key
	}
	if strings.HasPrefix(key, prefix) {
		return strings.TrimPrefix(key, prefix)
	}
	if !strings.HasSuffix(prefix, "/") {
		p := prefix + "/"
		if strings.HasPrefix(key, p) {
			return strings.TrimPrefix(key, p)
		}
	}
	return key
}

// joinKey 把相对路径拼到目标前缀下。
func joinKey(prefix, rel string) string {
	switch {
	case prefix == "":
		return rel
	case strings.HasSuffix(prefix, "/"):
		return prefix + rel
	default:
		return path.Join(prefix, rel)
	}
}

// =============== Mirror 主函数 ===============

// Mirror 把 cfg.Src 的对象同步到 cfg.Tgt，可选删除目标多余对象。
func Mirror(cfg MirrorOptions) error {
	if cfg.Src == nil || cfg.Tgt == nil {
		return fmt.Errorf("mirror: src and tgt are required")
	}
	if cfg.Src.Client == nil || cfg.Tgt.Client == nil ||
		cfg.Src.Client.S3 == nil || cfg.Tgt.Client.S3 == nil {
		return fmt.Errorf("mirror: src/tgt S3 client is nil")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.PartSizeMB <= 0 {
		cfg.PartSizeMB = 64
	}
	partSize := int64(cfg.PartSizeMB) * 1024 * 1024

	srcClient := cfg.Src.Client
	tgtClient := cfg.Tgt.Client
	srcBucket := cfg.Src.Bucket
	tgtBucket := cfg.Tgt.Bucket
	srcPrefix := cfg.Src.ObjectKey
	tgtPrefix := cfg.Tgt.ObjectKey

	// 防止同 client + 同 bucket + 同 prefix 的自映射
	if sameEndpoint(cfg.Src, cfg.Tgt) && srcBucket == tgtBucket && srcPrefix == tgtPrefix {
		return fmt.Errorf("mirror: source and target are the same location")
	}

	// 1. 列源 / 目标
	myprint.Printf("Listing source %s ...\n", srcClient.S3Path(srcBucket, srcPrefix))
	srcRaw, err := listAllObjects(srcClient, srcBucket, srcPrefix)
	if err != nil {
		return err
	}
	myprint.Printf("  source : %d objects\n", len(srcRaw))

	myprint.Printf("Listing target %s ...\n", tgtClient.S3Path(tgtBucket, tgtPrefix))
	tgtRaw, err := listAllObjects(tgtClient, tgtBucket, tgtPrefix)
	if err != nil {
		return err
	}
	myprint.Printf("  target : %d objects\n", len(tgtRaw))

	// 2. 转相对路径
	src := make(map[string]ObjectInfo, len(srcRaw))
	for k, v := range srcRaw {
		src[relKey(k, srcPrefix)] = v
	}
	tgt := make(map[string]ObjectInfo, len(tgtRaw))
	for k, v := range tgtRaw {
		tgt[relKey(k, tgtPrefix)] = v
	}

	// 3. 差异
	diff := calcDiff(src, tgt, cfg.Overwrite)
	myprint.Printf("Plan: %d to copy, %d to delete\n", len(diff.ToCopy), len(diff.ToDelete))
	if cfg.DryRun {
		for _, k := range diff.ToCopy {
			myprint.Printf("  COPY   %s -> %s\n",
				srcClient.S3Path(srcBucket, joinKey(srcPrefix, k)),
				tgtClient.S3Path(tgtBucket, joinKey(tgtPrefix, k)))
		}
		if cfg.Remove {
			for _, k := range diff.ToDelete {
				myprint.Printf("  DELETE %s\n", tgtClient.S3Path(tgtBucket, joinKey(tgtPrefix, k)))
			}
		}
		return nil
	}

	// 4. 并发复制（带进度条）
	sameEP := sameEndpoint(cfg.Src, cfg.Tgt)
	if sameEP {
		myprint.Println("Strategy: server-side CopyObject (same endpoint)")
	} else {
		myprint.Println("Strategy: download + upload (cross endpoint)")
	}

	pt := progress.New()
	pt.SetLabel("mirror")
	pt.Start()
	defer pt.Stop()

	// 预先设置总数（mirror 是先列完再操作的）
	pt.AddTotal(int64(len(diff.ToCopy)))

	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, cfg.Concurrency)
		copied  atomic.Int64
		skipped atomic.Int64
		failed  atomic.Int64
		startAt = time.Now()
	)

	for _, rel := range diff.ToCopy {
		if cfg.SizeLimit > 0 {
			if info, ok := src[rel]; ok && info.Size > cfg.SizeLimit {
				skipped.Add(1)
				myprint.Printf("SKIP (size > limit): %s (%s)\n", rel, FormatBytes(info.Size))
				continue
			}
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(rel string) {
			defer wg.Done()
			defer func() { <-sem }()

			srcKey := joinKey(srcPrefix, rel)
			tgtKey := joinKey(tgtPrefix, rel)

			var cerr error
			if sameEP {
				cerr = copyObjectSameEndpoint(srcClient, srcBucket, srcKey, tgtBucket, tgtKey)
			} else {
				cerr = copyObjectCrossEndpoint(srcClient, tgtClient, srcBucket, srcKey, tgtBucket, tgtKey, partSize)
			}
			if cerr != nil {
				msg := fmt.Sprintf("✗ %s → %s", srcClient.S3Path(srcBucket, srcKey), tgtClient.S3Path(tgtBucket, tgtKey))
				failed.Add(1)
				pt.AddFailed(msg)
				// pt.AddDone(1, 0, fmt.Sprintf("✗ %s → %s", srcClient.S3Path(srcBucket, srcKey), tgtClient.S3Path(tgtBucket, tgtKey)))
				pt.AddTotalDone(1)
				return
			}
			copied.Add(1)
			// pt.AddDone(1, 0, fmt.Sprintf("✓ %s → %s", srcClient.S3Path(srcBucket, srcKey), tgtClient.S3Path(tgtBucket, tgtKey)))
			pt.AddTotalDone(1)
		}(rel)
	}
	wg.Wait()

	// 5. 删除目标多余对象
	var deleted int
	if cfg.Remove && len(diff.ToDelete) > 0 {
		fullKeys := make([]string, 0, len(diff.ToDelete))
		for _, rel := range diff.ToDelete {
			fullKeys = append(fullKeys, joinKey(tgtPrefix, rel))
		}
		myprint.Printf("Deleting %d extra objects on target...\n", len(fullKeys))
		if err := deleteObjectsBatch(tgtClient, tgtBucket, fullKeys); err != nil {
			myprint.PrintfRed("delete error: %v\n", err)
		} else {
			deleted = len(fullKeys)
		}
	}

	myprint.PrintfGreen("Mirror done in %s: copied=%d, skipped=%d, failed=%d, deleted=%d\n",
		time.Since(startAt).Truncate(time.Millisecond),
		copied.Load(), skipped.Load(), failed.Load(), deleted,
	)
	if failed.Load() > 0 {
		return fmt.Errorf("mirror finished with %d failures", failed.Load())
	}
	return nil
}
