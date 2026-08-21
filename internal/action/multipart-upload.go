// multipart-upload.go 实现远端分片上传管理: 列出 (MpuList) 与中止 (MpuAbort) 服务端
// 进行中的 multipart upload.

package action

import (
	"context"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// mpuListPageSize 是 ListMultipartUploads 的单页请求上限。
// S3 服务端默认最多返回 1000 条, 显式设置避免依赖后端默认值。
const mpuListPageSize = 1000

// listAllMultipartUploads 翻页列举 prefix 下所有进行中的分片上传。
//
// ListMultipartUploads 单次最多返回 MaxUploads (服务端上限 1000) 条,
// 超过后通过 KeyMarker/UploadIDMarker -> NextKeyMarker/NextUploadIDMarker
// 驱动翻页; 不做翻页会漏掉第 1000 条之后的上传 (MpuList 少列、MpuAbort/rm -I 漏清)。
func (c *Action) listAllMultipartUploads(ctx context.Context, bucket, prefix string) ([]s3iface.UploadInfo, error) {
	var (
		all            []s3iface.UploadInfo
		keyMarker      string
		uploadIDMarker string
	)
	for {
		out, err := c.S3.ListMultipartUploads(ctx, bucket, &s3iface.ListMultipartUploadsOptions{
			Prefix:         prefix,
			MaxUploads:     mpuListPageSize,
			KeyMarker:      keyMarker,
			UploadIDMarker: uploadIDMarker,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, out.Uploads...)
		if !out.IsTruncated {
			return all, nil
		}
		keyMarker = out.NextKeyMarker
		uploadIDMarker = out.NextUploadIDMarker
		// 防御: 后端标记 IsTruncated 却不返回下一页 marker 时无法继续,
		// 停止翻页避免死循环 (已收集的结果保留)。
		if keyMarker == "" && uploadIDMarker == "" {
			return all, nil
		}
	}
}

// listAllParts 分页列举一个分片上传的全部已上传分片。
//
// ListParts 单页服务端上限为 1000 (AWS 对更大的 max-parts 返回 InvalidArgument),
// 超过后通过 PartNumberMarker -> NextPartNumberMarker 驱动翻页;
// 不翻页会在 >1000 片时拿到截断结果, 导致续传对账错位。
func (c *Action) listAllParts(ctx context.Context, bucket, key, uploadID string) ([]s3iface.PartInfo, error) {
	var all []s3iface.PartInfo
	marker := 0
	for {
		out, err := c.S3.ListParts(ctx, bucket, key, uploadID, marker, mpuListPageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, out.Parts...)
		if !out.IsTruncated || out.NextPartNumberMarker == 0 {
			return all, nil
		}
		marker = out.NextPartNumberMarker
	}
}

// MpuListOptions mpu list 命令参数.
type MpuListOptions struct {
	JSON bool // --json: JSON lines 输出
}

// MpuList 列出未完成的 multipart upload
func (c *Action) MpuList(opt MpuListOptions, bucket, prefix string) error {
	uploads, err := c.listAllMultipartUploads(c.Ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("list multipart uploads: %s", FormatAPIError(err))
	}
	var count int
	tbl := newLsTable(i18n.T("Upload ID", "上传ID"))
	for _, u := range uploads {
		count++
		initiated := ""
		if !u.Initiated.IsZero() {
			initiated = u.Initiated.Format(lsTimeLayout)
		}
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"path":      c.S3Path(bucket, u.Key),
				"bucket":    bucket,
				"key":       u.Key,
				"uploadId":  u.UploadID,
				"initiated": initiated,
			}); err != nil {
				return err
			}
			continue
		}
		tbl.add(lsRow{
			date:  initiated,
			size:  "-",
			typ:   "INCOMPLETE",
			path:  c.S3Path(bucket, u.Key),
			extra: u.UploadID,
			color: myprint.Yellow,
		})
	}
	if count == 0 {
		if opt.JSON {
			return nil
		}
		myprint.PrintfBoldYellow(i18n.T("%s: no in-progress multipart uploads\n", "%s：没有进行中的分段上传\n"), c.S3Path(bucket, ""))
		return nil
	}
	if !opt.JSON {
		tbl.render()
	}
	return nil
}

// MpuAbort 中止指定 uploadId，或一次性清空 prefix 下所有
func (c *Action) MpuAbort(bucket, prefix, uploadID string) error {
	// 单条指定 uploadId
	if uploadID != "" {
		// AbortMultipartUpload 需要 (Bucket, Key, UploadId) 三元组。
		// 若用户未提供具体的 object key（prefix 为空），尝试通过列举
		// 该 prefix 下的 in-progress uploads，找到匹配该 uploadId 的真实 key。
		key := prefix
		if key == "" {
			found, err := c.findUploadKey(bucket, prefix, uploadID)
			if err != nil {
				return err
			}
			if found == "" {
				return fmt.Errorf(i18n.T("abort mpu: object key is required for uploadId %q "+
					"(no matching in-progress upload found under %s); "+
					"run `mpu list` to find the key",
					"中止 mpu：uploadId %q 需要指定对象 key（%s 下未找到匹配的进行中上传）；"+
						"请运行 `mpu list` 查找 key"),
					uploadID, c.S3Path(bucket, prefix))
			}
			key = found
		}

		if err := c.S3.AbortMultipartUpload(c.Ctx, bucket, key, uploadID); err != nil {
			return fmt.Errorf("abort mpu: %s", FormatAPIError(err))
		}

		myprint.PrintfGreen(i18n.T("aborted: %s  uploadId=%s\n", "已中止：%s  uploadId=%s\n"), c.S3Path(bucket, key), uploadID)
		return nil
	}

	// 批量: 先翻页收集 prefix 下全部 in-progress uploads, 再逐个 abort。
	// 若边列举边 abort, 后页 marker 可能因前页被删而失效, 因此必须全部收集完再删。
	uploads, err := c.listAllMultipartUploads(c.Ctx, bucket, prefix)
	if err != nil {
		return fmt.Errorf("list mpu: %s", FormatAPIError(err))
	}
	var aborted, failed int
	for _, u := range uploads {
		if err := c.S3.AbortMultipartUpload(c.Ctx, bucket, u.Key, u.UploadID); err != nil {
			myprint.PrintfRed("abort %s/%s: %s\n", bucket, u.Key, FormatAPIError(err))
			failed++
			continue
		}
		aborted++
	}
	myprint.PrintfBoldGreen(i18n.T("aborted %d in-progress uploads under %s\n", "已中止 %d 个进行中的上传（位于 %s）\n"), aborted, c.S3Path(bucket, prefix))
	// 逐对象失败不能静默成功: 脚本依赖退出码判断是否清理干净。
	if failed > 0 {
		return fmt.Errorf(i18n.T("aborted %d of %d in-progress uploads: %d failed (see errors above)",
			"已中止 %d/%d 个进行中的上传：%d 个失败（见上方错误）"), aborted, len(uploads), failed)
	}
	return nil
}

// findUploadKey 在 prefix 下列举 in-progress multipart uploads，
func (c *Action) findUploadKey(bucket, prefix, uploadID string) (string, error) {
	uploads, err := c.listAllMultipartUploads(c.Ctx, bucket, prefix)
	if err != nil {
		return "", fmt.Errorf("list mpu: %s", FormatAPIError(err))
	}
	for _, u := range uploads {
		if u.UploadID == uploadID {
			return u.Key, nil
		}
	}
	return "", nil
}
