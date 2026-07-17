package action

import (
	"fmt"
	"io"
	"path"
	"s3cli/internal/utils"
	"s3cli/pkg/progress"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/s3api"
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

	Remove       bool     // 是否删除目标端多余的对象
	Overwrite    bool     // 已存在时是否依据 ETag/Size 覆盖
	DryRun       bool     // 仅打印将要做的事，不实际执行
	Concurrency  int      // 并发数 (默认 8)
	PartSizeMB   int      // 分片大小 MB (默认 64)
	SizeLimit    int64    // 单对象大小上限 (字节)，0 表示不限制
	MaxDelete    int      // --remove 时允许删除的最大对象数，0 表示不限制
	Include      []string // 仅同步匹配任一 glob 的相对 key；为空表示全部
	Exclude      []string // 不同步匹配任一 glob 的相对 key
	ManifestPath string   // 成功复制的相对 key 追加写入此文件
	Resume       bool     // 跳过 manifest 中已成功复制的 key
	NoProgress   bool     // 为 true 时不显示进度条（--quiet / 非终端场景）
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

	paginator := s3api.NewListObjectsV2Paginator(c.S3, bucket, &s3api.ListObjectsV2Options{Prefix: prefix})

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
			key := obj.Key
			info := ObjectInfo{
				Key:          relKey(key, prefix),
				Size:         obj.Size,
				ETag:         strings.Trim(obj.ETag, `"`),
				LastModified: obj.LastModified,
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

func matchesMirrorFilters(key string, include, exclude []string) bool {
	for _, pattern := range exclude {
		if matchMirrorGlob(pattern, key) {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, pattern := range include {
		if matchMirrorGlob(pattern, key) {
			return true
		}
	}
	return false
}

func matchMirrorGlob(pattern, key string) bool {
	if ok, _ := path.Match(pattern, key); ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, _ := path.Match(pattern, path.Base(key))
		return ok
	}
	return false
}

func filterObjects(in <-chan ObjectInfo, include, exclude []string) <-chan ObjectInfo {
	out := make(chan ObjectInfo, 1024)
	go func() {
		defer close(out)
		for obj := range in {
			if matchesMirrorFilters(obj.Key, include, exclude) {
				out <- obj
			}
		}
	}()
	return out
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
	sc, err1 := src.Client.GetS3Credentials()
	tc, err2 := tgt.Client.GetS3Credentials()
	if err1 != nil || err2 != nil {
		return false
	}
	// 规范化：去掉尾部斜杠，避免 "http://s3.example.com" 与 "http://s3.example.com/" 被判为不同
	normalize := func(e string) string { return strings.TrimRight(e, "/") }
	return strings.EqualFold(normalize(sc.BaseEndpoint), normalize(tc.BaseEndpoint))
}

// copyObjectSameEndpoint 同 endpoint 服务端复制。
func copyObjectSameEndpoint(c *S3Client, srcBucket, srcKey, tgtBucket, tgtKey string) error {
	_, err := c.S3.CopyObject(c.Ctx, srcBucket, srcKey, tgtBucket, tgtKey, &s3api.CopyObjectOptions{MetadataDirective: "COPY"})
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
	headResp, err := src.S3.HeadObject(src.Ctx, srcBucket, srcKey, "")
	if err != nil {
		return fmt.Errorf("head %s: %s", src.S3Path(srcBucket, srcKey), FormatAPIError(err))
	}

	totalSize := headResp.ContentLength
	if totalSize <= partSize {
		return copySingleCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey)
	}
	return copyMultipartCrossEndpoint(src, tgt, srcBucket, srcKey, tgtBucket, tgtKey, totalSize, partSize, headResp, report)
}

func copySingleCrossEndpoint(src, tgt *S3Client, srcBucket, srcKey, tgtBucket, tgtKey string) error {
	getResp, err := src.S3.GetObject(src.Ctx, srcBucket, srcKey, nil)
	if err != nil {
		return fmt.Errorf("get s3://%s/%s: %s", srcBucket, srcKey, FormatAPIError(err))
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(getResp.Body)

	// PutObject 在签名前需要计算 payload hash，要求 body 可 seek。
	// HTTP 响应流不可 seek，故先读入内存再用可 seek 的 bytes.Reader 上传。
	data, err := io.ReadAll(getResp.Body)
	if err != nil {
		return fmt.Errorf("read s3://%s/%s: %v", srcBucket, srcKey, err)
	}

	if _, err := tgt.S3.PutObject(tgt.Ctx, tgtBucket, tgtKey, data, &s3api.PutObjectOptions{
		ContentType:        getResp.ContentType,
		CacheControl:       getResp.CacheControl,
		ContentDisposition: getResp.ContentDisposition,
		ContentEncoding:    getResp.ContentEncoding,
		ContentLanguage:    getResp.ContentLanguage,
		Metadata:           getResp.Metadata,
	}); err != nil {
		return fmt.Errorf("put s3://%s/%s: %s", tgtBucket, tgtKey, FormatAPIError(err))
	}
	return nil
}

func copyMultipartCrossEndpoint(
	src, tgt *S3Client,
	srcBucket, srcKey, tgtBucket, tgtKey string,
	totalSize, partSize int64,
	head *s3api.HeadObjectOutput,
	report func(n int64),
) (err error) {
	createResp, err := tgt.S3.CreateMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, &s3api.PutObjectOptions{
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
	uploadID := createResp.UploadID

	defer func() {
		if err != nil {
			_ = tgt.S3.AbortMultipartUpload(tgt.Ctx, tgtBucket, tgtKey, uploadID)
		}
	}()

	var completed []s3api.CompletedPart
	partNum := int32(1)
	for offset := int64(0); offset < totalSize; offset += partSize {
		end := offset + partSize - 1
		if end >= totalSize {
			end = totalSize - 1
		}
		rangeStr := fmt.Sprintf("bytes=%d-%d", offset, end)

		getResp, getErr := src.S3.GetObject(src.Ctx, srcBucket, srcKey, &s3api.GetObjectOptions{Range: rangeStr})
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

		completed = append(completed, s3api.CompletedPart{
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

func deleteObjectsBatch(c *S3Client, bucket string, keys []string) error {
	const batchSize = 1000
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]
		objs := make([]s3api.ObjectIdentifier, len(batch))
		for j, k := range batch {
			objs[j] = s3api.ObjectIdentifier{Key: k}
		}
		_, err := c.S3.DeleteObjects(c.Ctx, bucket, objs, true)
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

// mirrorPlan 是校验并解析后的 mirror 执行计划, 供各阶段函数共用, 避免长参数列表。
type mirrorPlan struct {
	cfg       MirrorOptions
	srcClient *S3Client
	tgtClient *S3Client
	srcBucket string
	tgtBucket string
	srcPrefix string
	tgtPrefix string
	partSize  int64
	sameEP    bool
}

// Mirror 把 cfg.Src 的对象同步到 cfg.Tgt，可选删除目标多余对象。
func Mirror(cfg MirrorOptions) error {
	plan, err := resolveMirrorPlan(cfg)
	if err != nil {
		return err
	}
	manifest, err := openMirrorManifest(cfg.ManifestPath, cfg.Resume)
	if err != nil {
		return fmt.Errorf("mirror manifest: %w", err)
	}
	defer func(manifest *mirrorManifest) {
		_ = manifest.close()
	}(manifest)

	myprint.Printf("Listing & mirroring %s -> %s ...\n",
		plan.srcClient.S3Path(plan.srcBucket, plan.srcPrefix),
		plan.tgtClient.S3Path(plan.tgtBucket, plan.tgtPrefix))
	if plan.sameEP {
		myprint.Println("Strategy: server-side CopyObject (same endpoint)")
	} else {
		myprint.Println("Strategy: download + upload (cross endpoint)")
	}

	// 1. 流式列举源 / 目标（不缓存全集）
	listErrCh := make(chan error, 2)
	srcCh := make(chan ObjectInfo, 1024)
	tgtCh := make(chan ObjectInfo, 1024)
	go streamObjects(plan.srcClient, plan.srcBucket, plan.srcPrefix, srcCh, listErrCh)
	go streamObjects(plan.tgtClient, plan.tgtBucket, plan.tgtPrefix, tgtCh, listErrCh)
	filteredSrc := filterObjects(srcCh, cfg.Include, cfg.Exclude)
	filteredTgt := filterObjects(tgtCh, cfg.Include, cfg.Exclude)

	// 2. 流式归并差异
	actions := make(chan diffAction, 1024)
	go streamDiff(filteredSrc, filteredTgt, cfg.Overwrite, actions)

	// 3. DryRun：边归并边打印，无需缓存。
	if cfg.DryRun {
		return plan.dryRun(actions, listErrCh)
	}

	// 4. 并发复制 + 5. 删除多余对象
	return plan.copyAndDelete(actions, listErrCh, manifest)
}

// resolveMirrorPlan 校验入参并解析目标前缀, 返回可执行的 mirrorPlan。
func resolveMirrorPlan(cfg MirrorOptions) (*mirrorPlan, error) {
	if cfg.Src == nil || cfg.Tgt == nil {
		return nil, fmt.Errorf("mirror: src and tgt are required")
	}
	if cfg.Src.Client == nil || cfg.Tgt.Client == nil ||
		cfg.Src.Client.S3 == nil || cfg.Tgt.Client.S3 == nil {
		return nil, fmt.Errorf("mirror: src/tgt S3 client is nil")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = defaultConcurrency
	}
	if cfg.PartSizeMB <= 0 {
		cfg.PartSizeMB = 64
	}

	tgtClient := cfg.Tgt.Client
	tgtBucket := cfg.Tgt.Bucket
	srcPrefix := cfg.Src.ObjectKey
	tgtPrefix := cfg.Tgt.ObjectKey

	// 套用与 cp/mv 一致的目标前缀解析规则（mirror 源恒为目录树, appendRel 恒为 true,
	// 仅前缀可能因 trailing 语义而追加源目录名 —— 见规则 4）。
	state, err := tgtClient.DestStateOf(tgtBucket, tgtPrefix)
	if err != nil {
		state = utils.DestNone
	}
	tgtPrefix, _ = utils.ResolveDirDestPrefix(
		srcPrefix, cfg.Src.TrailingSlash,
		tgtPrefix, cfg.Tgt.TrailingSlash,
		state,
	)

	// 防止同 client + 同 bucket + 同 prefix 的自映射
	if sameEndpoint(cfg.Src, cfg.Tgt) && cfg.Src.Bucket == tgtBucket && srcPrefix == tgtPrefix {
		return nil, fmt.Errorf("mirror: source and target are the same location")
	}

	return &mirrorPlan{
		cfg:       cfg,
		srcClient: cfg.Src.Client,
		tgtClient: tgtClient,
		srcBucket: cfg.Src.Bucket,
		tgtBucket: tgtBucket,
		srcPrefix: srcPrefix,
		tgtPrefix: tgtPrefix,
		partSize:  int64(cfg.PartSizeMB) * 1024 * 1024,
		sameEP:    sameEndpoint(cfg.Src, cfg.Tgt),
	}, nil
}

// dryRun 边归并边打印计划, 不实际执行。
func (p *mirrorPlan) dryRun(actions <-chan diffAction, listErrCh chan error) error {
	var nCopy, nDelete int64
	for a := range actions {
		if a.delete {
			if p.cfg.Remove {
				myprint.Printf("  DELETE %s\n", p.tgtClient.S3Path(p.tgtBucket, joinKey(p.tgtPrefix, a.rel)))
			}
			nDelete++
			continue
		}
		myprint.Printf("  COPY   %s -> %s\n",
			p.srcClient.S3Path(p.srcBucket, joinKey(p.srcPrefix, a.rel)),
			p.tgtClient.S3Path(p.tgtBucket, joinKey(p.tgtPrefix, a.rel)))
		nCopy++
	}
	if err := drainListErr(listErrCh); err != nil {
		return err
	}
	myprint.Printf("Plan: %d to copy, %d to delete\n", nCopy, nDelete)
	return nil
}

// copyAndDelete 并发复制（带进度条），随后删除目标多余对象。
func (p *mirrorPlan) copyAndDelete(actions <-chan diffAction, listErrCh chan error, manifest *mirrorManifest) error {
	pt := progress.New()
	pt.SetLabel("mirror")
	if p.cfg.NoProgress {
		pt.SetQuiet()
	}
	pt.Start()
	defer pt.Stop()

	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, p.cfg.Concurrency)
		copied  atomic.Int64
		skipped atomic.Int64
		failed  atomic.Int64
		startAt = time.Now()

		toDelete []string // 仅 --remove 时累积；O(删除数) 而非 O(全集)
	)

	for a := range actions {
		if a.delete {
			if p.cfg.Remove {
				toDelete = append(toDelete, joinKey(p.tgtPrefix, a.rel))
			}
			continue
		}
		if p.cfg.Resume && manifest.has(a.rel) {
			skipped.Add(1)
			continue
		}

		if p.cfg.SizeLimit > 0 && a.size > p.cfg.SizeLimit {
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
			if p.copyOne(pt, rel, objSize, &copied, &failed) && manifest != nil {
				if err := manifest.mark(rel); err != nil {
					failed.Add(1)
					pt.AddFailed(1, fmt.Sprintf("✗ persist mirror manifest for %s: %v", rel, err))
				}
			}
		}(a.rel, a.size)
	}
	wg.Wait()

	// 列举过程中若出错，归并会提前结束 —— 优先报告列举错误。
	if err := drainListErr(listErrCh); err != nil {
		return err
	}
	if p.srcClient.Ctx.Err() != nil || p.tgtClient.Ctx.Err() != nil {
		return p.srcClient.Ctx.Err()
	}
	if failed.Load() > 0 {
		return fmt.Errorf("mirror finished with %d copy failures; target deletions were skipped", failed.Load())
	}

	// 删除目标多余对象
	var deleted int
	if p.cfg.Remove && len(toDelete) > 0 {
		if p.cfg.MaxDelete > 0 && len(toDelete) > p.cfg.MaxDelete {
			return fmt.Errorf("mirror planned to delete %d objects, exceeding --max-delete=%d", len(toDelete), p.cfg.MaxDelete)
		}
		myprint.Printf("Deleting %d extra objects on target...\n", len(toDelete))
		if err := deleteObjectsBatch(p.tgtClient, p.tgtBucket, toDelete); err != nil {
			myprint.PrintfRed("delete error: %v\n", err)
		} else {
			deleted = len(toDelete)
		}
	}

	myprint.PrintfGreen("Mirror done in %s: copied=%d, skipped=%d, failed=%d, deleted=%d\n",
		time.Since(startAt).Truncate(time.Millisecond),
		copied.Load(), skipped.Load(), failed.Load(), deleted,
	)
	return nil
}

// copyOne 复制单个对象并维护进度 / 计数（在 worker goroutine 中调用）。
func (p *mirrorPlan) copyOne(pt *progress.Tracker, rel string, objSize int64, copied, failed *atomic.Int64) bool {
	srcKey := joinKey(p.srcPrefix, rel)
	tgtKey := joinKey(p.tgtPrefix, rel)

	// report 实时上报本对象传输的字节增量；reported 用于成功对账 / 失败回退。
	var reported int64
	report := func(n int64) {
		if n == 0 {
			return
		}
		atomic.AddInt64(&reported, n)
		pt.AddTotalSizeDone(n)
	}

	var err error
	if p.sameEP {
		// 服务端 CopyObject 无分片进度，不传 report，靠成功后对账补齐。
		err = copyObjectSameEndpoint(p.srcClient, p.srcBucket, srcKey, p.tgtBucket, tgtKey)
	} else {
		err = copyObjectCrossEndpoint(p.srcClient, p.tgtClient, p.srcBucket, srcKey, p.tgtBucket, tgtKey, p.partSize, report)
	}
	msg := fmt.Sprintf("%s → %s", p.srcClient.S3Path(p.srcBucket, srcKey), p.tgtClient.S3Path(p.tgtBucket, tgtKey))
	if err != nil {
		// 失败：回退已上报字节，避免虚增进度。
		if r := atomic.LoadInt64(&reported); r != 0 {
			pt.AddTotalSizeDone(-r)
		}
		// 用户主动取消（Ctrl+C）导致的在途错误不计为失败，静默跳过。
		if IsCanceled(err) || p.srcClient.Ctx.Err() != nil {
			return false
		}
		failed.Add(1)
		pt.AddFailed(1, fmt.Sprintf("%s: %s", msg, err))
		return false
	}
	// 成功：对账，把进度精确补齐到 objSize（适配服务端 copy / 跨端分片偏差）。
	if d := objSize - atomic.LoadInt64(&reported); d != 0 {
		pt.AddTotalSizeDone(d)
	}
	copied.Add(1)
	pt.AddTotalDone(1, msg)
	return true
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
