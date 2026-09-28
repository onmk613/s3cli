// batch-delete.go 提供全代码库唯一的批量删除实现。
//
// 此前有三份几乎相同的实现 (object-del.go 的 deleteBatch、mirror-copy.go 的
// deleteObjectsBatch、bucket-remove.go 的内联循环), 批大小在三处是裸字面量 1000、
// 在另一处是常量, 而且都只报 Errors[0] —— 一次批量删除里其余失败项被静默丢弃。

package action

import (
	"context"
	"fmt"
	"strings"

	"s3cli/internal/api"
)

// s3DeleteBatchSize 是 DeleteObjects 单次请求的对象数上限 (S3 协议限制)。
const s3DeleteBatchSize = 1000

// s3DeleteReportLimit 是批量删除出错时在错误信息里展开的失败项条数上限,
// 避免一次 1000 个失败把错误串撑成几千行。
const s3DeleteReportLimit = 5

// deleteObjectsInBatches 按 S3 上限分批删除对象/版本, 自动分块。
//
// quiet=true 让服务端只在 Errors 里回传失败项 (成功项不回传, 省流量)。
// 返回的错误同时给出失败总数与前若干条明细; 错误链上保留第一个
// *api.ErrorResponse, 以便上层映射语义化退出码。
func (c *Action) deleteObjectsInBatches(ctx context.Context, bucket string, objects []api.ObjectIdentifier) error {
	return c.deleteObjectsInBatchesWith(ctx, bucket, objects, s3DeleteBatchSize)
}

// deleteObjectsInBatchesWith 与 deleteObjectsInBatches 相同, 但可指定批大小 (测试用)。
func (c *Action) deleteObjectsInBatchesWith(ctx context.Context, bucket string, objects []api.ObjectIdentifier, batchSize int) error {
	if batchSize <= 0 || batchSize > s3DeleteBatchSize {
		batchSize = s3DeleteBatchSize
	}
	for i := 0; i < len(objects); i += batchSize {
		end := min(i+batchSize, len(objects))
		batch := objects[i:end]

		result, err := c.S3.DeleteObjects(ctx, bucket, batch, true)
		if err != nil {
			return fmt.Errorf("delete batch of %d from %s: %w", len(batch), c.S3Path(bucket, ""), err)
		}
		if len(result.Errors) == 0 {
			continue
		}

		// quiet 模式下部分对象仍可能失败 (权限/瞬态错误), 静默跳过会让
		// `rm -r` 与 `mirror --remove` 的结果失真, 也会让随后的 DeleteBucket
		// 报 BucketNotEmpty 而用户不知道是谁没删掉。
		var detail strings.Builder
		for n, e := range result.Errors {
			if n == s3DeleteReportLimit {
				fmt.Fprintf(&detail, " (+%d more)", len(result.Errors)-s3DeleteReportLimit)
				break
			}
			if n > 0 {
				detail.WriteString("; ")
			}
			fmt.Fprintf(&detail, "%q: %s: %s", e.Key, e.Code, e.Message)
		}
		first := result.Errors[0]
		return fmt.Errorf("%d of %d object(s) failed to delete from %s [%s]: %w",
			len(result.Errors), len(batch), c.S3Path(bucket, ""), detail.String(),
			&api.ErrorResponse{Code: first.Code, Message: first.Message, Key: first.Key, StatusCode: 400})
	}
	return nil
}

// deleteKeysInBatches 是 deleteObjectsInBatches 的便捷形式: 只按 key 删除 (无版本)。
// mirror --remove / bucket remove 等只需要 key 的场景用它, 免得每处都手写
// []string -> []api.ObjectIdentifier 的转换。
func (c *Action) deleteKeysInBatches(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objects := make([]api.ObjectIdentifier, len(keys))
	for i, k := range keys {
		objects[i] = api.ObjectIdentifier{Key: k}
	}
	return c.deleteObjectsInBatches(ctx, bucket, objects)
}
