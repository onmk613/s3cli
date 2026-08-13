// object-du.go 实现磁盘占用统计 DuObject:
// 默认输出前缀总大小; --recursive/-r 按目录层级输出各前缀占用, --depth/-d 限制层级.

package action

import (
	"sort"
	"strconv"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// DuOptions du 命令参数.
type DuOptions struct {
	Recursive bool // -r: 逐目录输出占用
	Depth     int  // -d: 仅统计 N 层以内的目录 (与 --recursive 搭配, 根目录为第 1 层)
	JSON      bool // --json: JSON lines 输出
}

// DuObject 显示磁盘占用, 只支持 bucket 及以下级别.
func (c *Action) DuObject(opt DuOptions, bucket, prefix string) error {
	if opt.Recursive {
		return c.duRecursive(opt, bucket, prefix)
	}

	var totalSize, count int64
	err := c.forEachObject(c.Ctx, bucket, prefix, func(o s3iface.ObjectInfo) error {
		totalSize += o.Size
		count++
		return nil
	})
	if err != nil {
		return err
	}

	if opt.JSON {
		return printJSONLine(map[string]any{
			"path":    c.S3Path(bucket, prefix),
			"fileNum": count,
			"size":    totalSize,
		})
	}

	tbl := myprint.NewTable(i18n.T("Path", "路径"), i18n.T("Objects", "对象数"), i18n.T("Size", "大小")).AlignRight(1, 2)
	tbl.AddRow(
		myprint.Cell{Text: c.S3Path(bucket, prefix), Color: myprint.BoldBlue},
		myprint.Cell{Text: strconv.FormatInt(count, 10)},
		myprint.Cell{Text: FormatBytes(totalSize)},
	)
	tbl.Render()
	return nil
}

// duRecursive 按目录层级统计 (-r):
// 一次性递归列举全部对象, 把每个对象的大小累加到其每一级祖先目录,
// 再按层级从深到浅输出各目录的总占用.
func (c *Action) duRecursive(opt DuOptions, bucket, prefix string) error {
	base := strings.TrimSuffix(prefix, "/")

	// dirTotal[dir] = 该目录 (含子级) 的对象总大小与个数; dirSet 记录真实目录
	// (某个对象 key 的任一祖先前缀; 仅有文件身份而没有子级的 key 不进入).
	dirTotal := map[string]int64{}
	dirCount := map[string]int64{}
	dirSet := map[string]bool{}

	err := c.forEachObject(c.Ctx, bucket, prefix, func(o s3iface.ObjectInfo) error {
		key := o.Key
		sz := o.Size
		// 相对 prefix 的路径段
		rel := strings.TrimPrefix(key, prefix)
		rel = strings.TrimPrefix(rel, "/")
		segments := strings.Split(rel, "/")
		// 每个对象都为所有祖先目录 (不含自身) 打标记并累加大小
		for i := 1; i < len(segments); i++ {
			dir := strings.Join(segments[:i], "/")
			dirSet[dir] = true
			dirTotal[dir] += sz
			dirCount[dir]++
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 输出顺序: 层级从深到浅, 同层按名称; 根目录最后输出
	dirs := make([]string, 0, len(dirTotal))
	for d := range dirTotal {
		if !dirSet[d] {
			continue // 只是文件 key, 不是目录
		}
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		li, lj := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/")
		if li != lj {
			return li > lj // 深的在前
		}
		return dirs[i] < dirs[j]
	})

	var dirErr error
	tbl := myprint.NewTable(i18n.T("Path", "路径"), i18n.T("Objects", "对象数"), i18n.T("Size", "大小")).AlignRight(1, 2)
	printDir := func(dir string) {
		// 相对命令行参数的第 1 层子目录为层级 1; --depth N 仅输出层级 < N 的目录
		// (即 -d 1 只输出参数本身, -d 2 输出参数与其直接子目录).
		if opt.Depth > 0 {
			relLevel := strings.Count(dir, "/") + 1
			if relLevel >= opt.Depth {
				return
			}
		}
		display := dir
		if base != "" {
			display = base + "/" + dir
		}
		if opt.JSON {
			if err := printJSONLine(map[string]any{
				"path":    c.S3Path(bucket, display),
				"fileNum": dirCount[dir],
				"size":    dirTotal[dir],
				"dir":     true,
			}); err != nil {
				dirErr = err
				return
			}
			return
		}
		tbl.AddRow(
			myprint.Cell{Text: c.S3Path(bucket, display), Color: myprint.BoldBlue},
			myprint.Cell{Text: strconv.FormatInt(dirCount[dir], 10)},
			myprint.Cell{Text: FormatBytes(dirTotal[dir])},
		)
	}

	for _, d := range dirs {
		printDir(d)
		if dirErr != nil {
			return dirErr
		}
	}

	// 根目录 (prefix 本身) 始终输出
	rootTotal, rootCount, err := c.prefixUsage(bucket, prefix)
	if err != nil {
		return err
	}
	if opt.JSON {
		return printJSONLine(map[string]any{
			"path":    c.S3Path(bucket, prefix),
			"fileNum": rootCount,
			"size":    rootTotal,
			"dir":     true,
		})
	}
	tbl.AddRow(
		myprint.Cell{Text: c.S3Path(bucket, prefix), Color: myprint.BoldBlue},
		myprint.Cell{Text: strconv.FormatInt(rootCount, 10)},
		myprint.Cell{Text: FormatBytes(rootTotal)},
	)
	tbl.Render()
	return nil
}

// prefixUsage 统计前缀下对象总数与总大小.
func (c *Action) prefixUsage(bucket, prefix string) (total int64, count int64, err error) {
	err = c.forEachObject(c.Ctx, bucket, prefix, func(o s3iface.ObjectInfo) error {
		total += o.Size
		count++
		return nil
	})
	return total, count, err
}
