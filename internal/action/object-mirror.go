// object-mirror.go 实现 mirror (双端镜像同步) 的主流程: 参数校验、计划解析、
// 流式列举源/目标、归并差异、并发复制与删除多余对象.
//
// 列举/归并/过滤/重映射原语见 mirror-stream.go; 复制/删除原语见 mirror-copy.go;
// 断点续传用的 manifest 见 mirror-manifest.go.
//
// 整体设计: 源与目标各自流式列举 (不缓存全集), 经 streamDiff 做有序归并产出
// diffAction 流, 再由并发 worker 执行 copy/delete, 内存占用恒定.

package action

import (
	"fmt"
	"s3cli/internal/s3path"
	"s3cli/pkg/progress"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	myprint "s3cli/pkg/fmtutil"
)

// =============== 配置 ===============

// S3PathOptions 描述一个 S3 端点 + bucket + key 前缀.
//
// 这里持有的是 *Action (而非裸 *s3.Client), 与单端 action (copy/mv/...)
// 完全一致: 天然携带 Ctx / Alias / GetCreds() 等上下文, 便于双端统一处理.
type S3PathOptions struct {
	Client        *Action
	Bucket        string
	ObjectKey     string // 作为前缀; 可为空表示整个 bucket
	TrailingSlash bool   // 输入是否以 "/" 结尾 (语义上是 "目录")
}

// MirrorOptions Mirror 主入口参数.
type MirrorOptions struct {
	Src *S3PathOptions
	Tgt *S3PathOptions

	Remove       bool     // 是否删除目标端多余的对象
	Overwrite    bool     // 已存在时是否依据 ETag/Size 覆盖
	DryRun       bool     // 仅打印将要做的事, 不实际执行
	Concurrency  int      // 并发数 (默认 defaultConcurrency)
	PartSizeMB   int      // 分片大小 MB (经 multipartPartSize 钳制, 最小 5MB)
	StorageClass string   // 目标对象的存储类别 (STANDARD / STANDARD_IA / GLACIER / ...)
	SizeLimit    int64    // 单对象大小上限 (字节), 0 表示不限制
	MaxDelete    int      // --remove 时允许删除的最大对象数, 0 表示不限制
	Include      []string // 仅同步匹配任一 glob 的相对 key; 为空表示全部
	Exclude      []string // 不同步匹配任一 glob 的相对 key
	ManifestPath string   // 成功复制的相对 key 追加写入此文件
	Resume       bool     // 跳过 manifest 中已成功复制的 key
	NoProgress   bool     // 为 true 时不显示进度条 (--quiet / 非终端场景)
}

// =============== 计划 ===============

// mirrorPlan 是校验并解析后的 mirror 执行计划, 供各阶段函数共用, 避免长参数列表.
type mirrorPlan struct {
	cfg       MirrorOptions
	srcClient *Action
	tgtClient *Action
	srcBucket string
	tgtBucket string
	srcPrefix string
	tgtPrefix string
	partSize  int64
	sameEP    bool
}

// Mirror 把 cfg.Src 的对象同步到 cfg.Tgt, 可选删除目标多余对象.
func Mirror(cfg MirrorOptions) error {
	plan, err := resolveMirrorPlan(cfg)
	if err != nil {
		return err
	}

	// DryRun 不打开 manifest: O_CREATE 会凭空落盘一个文件, 属于不应有的副作用。
	var manifest *mirrorManifest
	if !cfg.DryRun {
		manifest, err = openMirrorManifest(cfg.ManifestPath, cfg.Resume)
		if err != nil {
			return fmt.Errorf("mirror manifest: %w", err)
		}
		defer func(manifest *mirrorManifest) {
			_ = manifest.close()
		}(manifest)
	}

	myprint.Printf("Mirroring %s -> %s ...\n",
		plan.srcClient.S3Path(plan.srcBucket, plan.srcPrefix),
		plan.tgtClient.S3Path(plan.tgtBucket, plan.tgtPrefix))
	if plan.sameEP {
		myprint.Println("Strategy: server-side CopyObject (same endpoint)")
	} else {
		myprint.Println("Strategy: download + upload (cross endpoint)")
	}

	// 1. 流式列举源 / 目标 (不缓存全集)
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

	// 3. DryRun: 边归并边打印, 无需缓存。
	if cfg.DryRun {
		return plan.dryRun(actions, listErrCh)
	}

	// 4. 并发复制 + 5. 删除多余对象
	return plan.copyAndDelete(actions, listErrCh, manifest)
}

// resolveMirrorPlan 校验入参并解析目标前缀, 返回可执行的 mirrorPlan.
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

	tgtClient := cfg.Tgt.Client
	tgtBucket := cfg.Tgt.Bucket
	srcPrefix := cfg.Src.ObjectKey
	tgtPrefix := cfg.Tgt.ObjectKey

	// 套用与 cp/mv 一致的目标前缀解析规则 (mirror 源恒为目录树, appendRel 恒为 true,
	// 仅前缀可能因 trailing 语义而追加源目录名 —— 见规则 4)。
	state, err := tgtClient.DestStateOf(tgtBucket, tgtPrefix)
	if err != nil {
		state = s3path.DestNone
	}
	tgtPrefix, _ = s3path.ResolveDirDestPrefix(
		srcPrefix, cfg.Src.TrailingSlash,
		tgtPrefix, cfg.Tgt.TrailingSlash,
		state,
	)

	// 源/目标前缀统一规范化为 "dir/" 形态, 避免裸前缀列举时的前缀碰撞
	// (src "dir" 会误匹配 "dir2/x"; tgt "out" 会在 --remove 时误删 "out2/y")。
	srcPrefix = normalizeMirrorPrefix(srcPrefix)
	tgtPrefix = normalizeMirrorPrefix(tgtPrefix)

	sameEP := sameEndpoint(cfg.Src, cfg.Tgt)
	// 同 endpoint + 同 bucket 时, 禁止源/目标前缀互相包含:
	//   - src ⊆ tgt: --remove 会把 src 独有的对象当作 "目标多余" 删掉 (删自己的数据);
	//     不加 --remove 也会边列举边复制, 新写入的对象被源分页器再次列出, 级联复制。
	//   - 完全相等是其中特例 (原自映射守卫)。
	if sameEP && cfg.Src.Bucket == tgtBucket &&
		(strings.HasPrefix(srcPrefix, tgtPrefix) || strings.HasPrefix(tgtPrefix, srcPrefix)) {
		return nil, fmt.Errorf("mirror: source and target prefixes overlap on the same bucket (%q vs %q)", srcPrefix, tgtPrefix)
	}

	return &mirrorPlan{
		cfg:       cfg,
		srcClient: cfg.Src.Client,
		tgtClient: tgtClient,
		srcBucket: cfg.Src.Bucket,
		tgtBucket: tgtBucket,
		srcPrefix: srcPrefix,
		tgtPrefix: tgtPrefix,
		// 分片大小统一走 multipartPartSize 钳制 (>=5MB), 与 put 路径一致。
		partSize: multipartPartSize(cfg.PartSizeMB, 0),
		sameEP:   sameEP,
	}, nil
}

// dryRun 边归并边打印计划, 不实际执行.
func (p *mirrorPlan) dryRun(actions <-chan diffAction, listErrCh chan error) error {
	var nCopy, nDelete, nExtra int64
	for a := range actions {
		if a.delete {
			if p.cfg.Remove {
				myprint.Printf("  DELETE %s\n", p.tgtClient.S3Path(p.tgtBucket, joinKey(p.tgtPrefix, a.rel)))
				nDelete++
			} else {
				// 未加 --remove 时这些对象不会被删, 单独统计, 避免计划摘要误导。
				nExtra++
			}
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
	if p.cfg.Remove {
		myprint.Printf("Plan: %d to copy, %d to delete\n", nCopy, nDelete)
	} else {
		myprint.Printf("Plan: %d to copy (%d extra on target, use --remove to delete)\n", nCopy, nExtra)
	}
	return nil
}

// copyAndDelete 并发复制 (带进度条), 随后删除目标多余对象.
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
		// manifest 持久化失败单独计数: 复制本身已成功, 不应计入 copy failed
		// (否则同一对象同时出现在 copied 和 failed 里, 且会错误地阻断删除阶段)。
		manifestFailed atomic.Int64
		startAt        = time.Now()

		toDelete []string // 仅 --remove 时累积; O(删除数) 而非 O(全集)
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
					manifestFailed.Add(1)
					pt.AddFailed(1, fmt.Sprintf("✗ persist mirror manifest for %s: %v", rel, err))
				}
			}
		}(a.rel, a.size)
	}
	wg.Wait()

	// 列举过程中若出错, 归并会提前结束 —— 优先报告列举错误。
	if err := drainListErr(listErrCh); err != nil {
		return err
	}
	// 返回实际被取消那一端的错误; 原实现恒返回 src 端的 Err(),
	// 仅 tgt 端取消时会错误地返回 nil (静默成功)。
	if err := p.srcClient.Ctx.Err(); err != nil {
		return err
	}
	if err := p.tgtClient.Ctx.Err(); err != nil {
		return err
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
	if mf := manifestFailed.Load(); mf > 0 {
		return fmt.Errorf("mirror: %d object(s) copied but not recorded into manifest; rerun without --resume or fix the manifest file", mf)
	}
	return nil
}

// copyOne 复制单个对象并维护进度 / 计数 (在 worker goroutine 中调用).
// 返回 true 表示复制成功 (调用方据此决定是否写入 manifest).
func (p *mirrorPlan) copyOne(pt *progress.Tracker, rel string, objSize int64, copied, failed *atomic.Int64) bool {
	srcKey := joinKey(p.srcPrefix, rel)
	tgtKey := joinKey(p.tgtPrefix, rel)

	// report 实时上报本对象传输的字节增量; reported 用于成功对账 / 失败回退。
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
		// 服务端 CopyObject 无分片进度, 不传 report, 靠成功后对账补齐。
		err = copyObjectSameEndpoint(p.srcClient, p.srcBucket, srcKey, p.tgtBucket, tgtKey, p.cfg.StorageClass)
	} else {
		err = copyObjectCrossEndpoint(p.srcClient, p.tgtClient, p.srcBucket, srcKey, p.tgtBucket, tgtKey, p.cfg.StorageClass, p.partSize, report)
	}
	msg := fmt.Sprintf("%s → %s", p.srcClient.S3Path(p.srcBucket, srcKey), p.tgtClient.S3Path(p.tgtBucket, tgtKey))
	if err != nil {
		// 失败: 回退已上报字节, 避免虚增进度。
		if r := atomic.LoadInt64(&reported); r != 0 {
			pt.AddTotalSizeDone(-r)
		}
		// 用户主动取消 (Ctrl+C) 导致的在途错误不计为失败, 静默跳过。
		if IsCanceled(err) || p.srcClient.Ctx.Err() != nil {
			return false
		}
		failed.Add(1)
		pt.AddFailed(1, fmt.Sprintf("%s: %s", msg, err))
		return false
	}
	// 成功: 对账, 把进度精确补齐到 objSize (适配服务端 copy / 跨端分片偏差)。
	if d := objSize - atomic.LoadInt64(&reported); d != 0 {
		pt.AddTotalSizeDone(d)
	}
	copied.Add(1)
	pt.AddTotalDone(1, msg)
	return true
}

// drainListErr 非阻塞读取列举阶段的首个错误 (若有).
// 用户主动取消引起的错误视为正常停止, 返回 nil.
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
