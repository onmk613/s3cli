package action

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/progress"
	"s3cli/pkg/utils"

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

// streamObjects 流式列出 bucket 下 prefix 的所有对象，把每个对象（相对路径）
// 按 S3 返回的字典序写入 out。S3 ListObjectsV2 保证 key 字典序递增；去掉
// 固定前缀后，相对 key 的相对顺序不变，因此 out 也是有序流。
//
// 该函数不在内存里缓存全集 —— 列举与下游消费同时进行，内存恒定。
// 出错时把错误写入 errCh 并提前关闭 out。
func streamObjects(c *S3Client, bucket, prefix string, out chan<- ObjectInfo, errCh chan<- error) {
	defer close(out)

	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			select {
			case errCh <- fmt.Errorf("list %s: %s", c.S3Path(bucket, prefix), FormatAPIError(err)):
			default:
			}
			return
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			info := ObjectInfo{
				Key:          relKey(key, prefix),
				Size:         aws.ToInt64(obj.Size),
				ETag:         strings.Trim(aws.ToString(obj.ETag), `"`),
				LastModified: aws.ToTime(obj.LastModified),
			}
			select {
			case out <- info:
			case <-c.Ctx.Done():
				select {
				case errCh <- c.Ctx.Err():
				default:
				}
				return
			}
		}
	}
}

// =============== 差异计算（流式归并）===============

// diffAction 描述归并产出的单个差异决策。
type diffAction struct {
	rel    string // 相对路径
	delete bool   // true=目标多余需删除；false=需 src->tgt 复制
	size   int64  // 复制时为源对象大小（用于进度统计）
}

// streamDiff 归并两个“有序”对象流，边读边产出差异决策到 actions。
//
// 因为两个流都按相对 key 字典序递增，可做经典 merge-join：
//   - src 当前 key < tgt 当前 key  -> src 独有 -> COPY
//   - src 当前 key > tgt 当前 key  -> tgt 独有 -> DELETE
//   - 相等                          -> 两端都有，overwrite 且有变更才 COPY
//
// 内存占用 O(1)（仅各持有一个待比较对象），不依赖全集装载。
func streamDiff(srcCh, tgtCh <-chan ObjectInfo, overwrite bool, actions chan<- diffAction) {
	defer close(actions)

	src, srcOK := <-srcCh
	tgt, tgtOK := <-tgtCh

	for srcOK && tgtOK {
		switch {
		case src.Key < tgt.Key:
			actions <- diffAction{rel: src.Key, size: src.Size}
			src, srcOK = <-srcCh
		case src.Key > tgt.Key:
			actions <- diffAction{rel: tgt.Key, delete: true}
			tgt, tgtOK = <-tgtCh
		default: // 相等
			if overwrite && needsUpdate(src, tgt) {
				actions <- diffAction{rel: src.Key, size: src.Size}
			}
			src, srcOK = <-srcCh
			tgt, tgtOK = <-tgtCh
		}
	}
	for srcOK { // 剩余源对象：目标都没有 -> COPY
		actions <- diffAction{rel: src.Key, size: src.Size}
		src, srcOK = <-srcCh
	}
	for tgtOK { // 剩余目标对象：源都没有 -> DELETE
		actions <- diffAction{rel: tgt.Key, delete: true}
		tgt, tgtOK = <-tgtCh
	}
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
// report 用于在传输过程中实时上报新增字节（增量），可为 nil。
func copyObjectCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
	partSize int64,
	report func(n int64),
) error {
	headResp, err := src.S3.HeadObject(src.Ctx, &s3.HeadObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("head %s: %s", src.S3Path(srcBucket, srcKey), FormatAPIError(err))
	}

	totalSize := aws.ToInt64(headResp.ContentLength)
	if totalSize <= partSize {
		return copySingleCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, report)
	}
	return copyMultipartCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, totalSize, partSize, headResp, report)
}

func copySingleCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
	report func(n int64),
) error {
	getResp, err := src.S3.GetObject(src.Ctx, &s3.GetObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %s", srcBucket, srcKey, FormatAPIError(err))
	}
	defer getResp.Body.Close()

	// PutObject 在签名前需要计算 payload hash，要求 body 可 seek。
	// HTTP 响应流不可 seek，故先读入内存再用可 seek 的 bytes.Reader 上传。
	data, err := io.ReadAll(getResp.Body)
	if err != nil {
		return fmt.Errorf("read s3://%s/%s: %v", srcBucket, srcKey, err)
	}

	// 用计数 reader 包装可 seek 的源，上传读取时实时上报已传字节。
	var body io.Reader = bytes.NewReader(data)
	if report != nil {
		body = NewUploadCounter(bytes.NewReader(data), report)
	}

	put := &s3.PutObjectInput{
		Bucket:             aws.String(tgtBucket),
		Key:                aws.String(tgtKey),
		Body:               body,
		ContentLength:      aws.Int64(int64(len(data))),
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
	report func(n int64),
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

		// UploadPart 同样需要可 seek 的 body 来计算 payload hash。
		partData, readErr := io.ReadAll(getResp.Body)
		getResp.Body.Close()
		if readErr != nil {
			err = fmt.Errorf("read part %d: %v", partNum, readErr)
			return err
		}

		uploadResp, upErr := tgt.S3.UploadPart(tgt.Ctx, &s3.UploadPartInput{
			Bucket:        aws.String(tgtBucket),
			Key:           aws.String(tgtKey),
			UploadId:      aws.String(uploadID),
			PartNumber:    aws.Int32(partNum),
			Body:          bytes.NewReader(partData),
			ContentLength: aws.Int64(int64(len(partData))),
		})
		if upErr != nil {
			err = fmt.Errorf("upload part %d: %s", partNum, FormatAPIError(upErr))
			return err
		}

		completed = append(completed, s3types.CompletedPart{
			PartNumber: aws.Int32(partNum),
			ETag:       uploadResp.ETag,
		})
		if report != nil {
			report(end - offset + 1) // 本分片字节数
		}
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

	// 套用与 cp/mv 一致的目标前缀解析规则（mirror 源恒为目录树, appendRel 恒为 true,
	// 仅前缀可能因 trailing 语义而追加源目录名 —— 见规则 4）。
	{
		state, err := tgtClient.DestStateOf(tgtBucket, tgtPrefix)
		if err != nil {
			state = utils.DestNone
		}
		effPrefix, _ := utils.ResolveDirDestPrefix(
			srcPrefix, cfg.Src.TrailingSlash,
			tgtPrefix, cfg.Tgt.TrailingSlash,
			state,
		)
		tgtPrefix = effPrefix
	}

	// 防止同 client + 同 bucket + 同 prefix 的自映射
	if sameEndpoint(cfg.Src, cfg.Tgt) && srcBucket == tgtBucket && srcPrefix == tgtPrefix {
		return fmt.Errorf("mirror: source and target are the same location")
	}

	verbose := !myprint.Quiet()
	sameEP := sameEndpoint(cfg.Src, cfg.Tgt)

	// 1. 流式列举源 / 目标（不缓存全集）
	if verbose {
		myprint.Printf("Listing & mirroring %s -> %s ...\n",
			srcClient.S3Path(srcBucket, srcPrefix), tgtClient.S3Path(tgtBucket, tgtPrefix))
		if sameEP {
			myprint.Println("Strategy: server-side CopyObject (same endpoint)")
		} else {
			myprint.Println("Strategy: download + upload (cross endpoint)")
		}
	}

	listErrCh := make(chan error, 2)
	srcCh := make(chan ObjectInfo, 1024)
	tgtCh := make(chan ObjectInfo, 1024)
	go streamObjects(srcClient, srcBucket, srcPrefix, srcCh, listErrCh)
	go streamObjects(tgtClient, tgtBucket, tgtPrefix, tgtCh, listErrCh)

	// 2. 流式归并差异
	actions := make(chan diffAction, 1024)
	go streamDiff(srcCh, tgtCh, cfg.Overwrite, actions)

	// 3. DryRun：边归并边打印，无需缓存。
	if cfg.DryRun {
		var nCopy, nDelete int64
		for a := range actions {
			if a.delete {
				if cfg.Remove {
					myprint.Printf("  DELETE %s\n", tgtClient.S3Path(tgtBucket, joinKey(tgtPrefix, a.rel)))
				}
				nDelete++
				continue
			}
			myprint.Printf("  COPY   %s -> %s\n",
				srcClient.S3Path(srcBucket, joinKey(srcPrefix, a.rel)),
				tgtClient.S3Path(tgtBucket, joinKey(tgtPrefix, a.rel)))
			nCopy++
		}
		if err := drainListErr(listErrCh); err != nil {
			return err
		}
		if verbose {
			myprint.Printf("Plan: %d to copy, %d to delete\n", nCopy, nDelete)
		}
		return nil
	}

	// 4. 并发复制（带进度条）。总量随归并进度增量发现，而非预先已知。
	pt := progress.New()
	pt.SetLabel("mirror")
	pt.Start()
	defer pt.Stop()

	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, cfg.Concurrency)
		copied  atomic.Int64
		skipped atomic.Int64
		failed  atomic.Int64
		startAt = time.Now()

		toDelete []string // 仅 --remove 时累积；O(删除数) 而非 O(全集)
	)

	for a := range actions {
		if a.delete {
			if cfg.Remove {
				toDelete = append(toDelete, joinKey(tgtPrefix, a.rel))
			}
			continue
		}

		if cfg.SizeLimit > 0 && a.size > cfg.SizeLimit {
			skipped.Add(1)
			myprint.Printf("SKIP (size > limit): %s (%s)\n", a.rel, FormatBytes(a.size))
			continue
		}

		pt.AddTotal(1)
		pt.AddTotalSize(a.size)

		wg.Add(1)
		sem <- struct{}{}
		go func(rel string, objSize int64) {
			defer wg.Done()
			defer func() { <-sem }()

			srcKey := joinKey(srcPrefix, rel)
			tgtKey := joinKey(tgtPrefix, rel)

			// report 实时上报本对象传输的字节增量；reported 用于成功对账 / 失败回退。
			var reported int64
			report := func(n int64) {
				if n == 0 {
					return
				}
				atomic.AddInt64(&reported, n)
				pt.AddTotalSizeDone(n)
			}

			var cerr error
			if sameEP {
				// 服务端 CopyObject 无分片进度，不传 report，靠成功后对账补齐。
				cerr = copyObjectSameEndpoint(srcClient, srcBucket, srcKey, tgtBucket, tgtKey)
			} else {
				cerr = copyObjectCrossEndpoint(srcClient, tgtClient, srcBucket, srcKey, tgtBucket, tgtKey, partSize, report)
			}
			if cerr != nil {
				// 失败：回退已上报字节，避免虚增进度。
				if r := atomic.LoadInt64(&reported); r != 0 {
					pt.AddTotalSizeDone(-r)
				}
				// 用户主动取消（Ctrl+C）导致的在途错误不计为失败，静默跳过。
				if IsCanceled(cerr) || srcClient.Ctx.Err() != nil {
					return
				}
				msg := fmt.Sprintf("✗ %s → %s: %v", srcClient.S3Path(srcBucket, srcKey), tgtClient.S3Path(tgtBucket, tgtKey), cerr)
				failed.Add(1)
				pt.AddFailed(msg)
				pt.AddTotalDone(1)
				return
			}
			// 成功：对账，把进度精确补齐到 objSize（适配服务端 copy / 跨端分片偏差）。
			if d := objSize - atomic.LoadInt64(&reported); d != 0 {
				pt.AddTotalSizeDone(d)
			}
			copied.Add(1)
			pt.AddTotalDone(1)
		}(a.rel, a.size)
	}
	wg.Wait()

	// 列举过程中若出错，归并会提前结束 —— 优先报告列举错误。
	if err := drainListErr(listErrCh); err != nil {
		return err
	}

	// 5. 删除目标多余对象
	var deleted int
	if cfg.Remove && len(toDelete) > 0 {
		myprint.Printf("Deleting %d extra objects on target...\n", len(toDelete))
		if err := deleteObjectsBatch(tgtClient, tgtBucket, toDelete); err != nil {
			myprint.PrintfRed("delete error: %v\n", err)
		} else {
			deleted = len(toDelete)
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

// drainListErr 非阻塞读取列举阶段的首个错误（若有）。
// 用户主动取消引起的错误视为正常停止，返回 nil。
func drainListErr(errCh chan error) error {
	select {
	case err := <-errCh:
		if IsCanceled(err) {
			return nil
		}
		return err
	default:
		return nil
	}
}
