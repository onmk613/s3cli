package render

import (
	"sort"

	"s3cli/internal/fmtutil"
)

// TreeNode 是树形渲染所需的最小节点视图。调用方负责把业务对象
// (分页结果、目录标记等) 转换成本结构, 渲染层不感知其来源。
type TreeNode struct {
	Name     string
	IsFile   bool
	Size     int64
	Children []TreeNode
}

// TreeOptions 控制树形渲染。
type TreeOptions struct {
	Files    bool // 是否显示文件 (默认只显示目录)
	ShowSize bool // 文件名旁显示大小
	MaxDepth int  // 最大深度, 0 = 不限
}

// TreeStats 是渲染过程中统计出的规模信息。
type TreeStats struct {
	Files     int
	Dirs      int
	TotalSize int64
}

// sortedChildren 返回排序后的子节点: 目录在前、文件在后, 同类按名称。
// 这个顺序是 tree 输出的既有约定, 渲染层与统计层必须共用同一份实现,
// 否则 --json 的计数会与文本模式的可见行不一致。
func (n *TreeNode) sortedChildren() []TreeNode {
	out := make([]TreeNode, len(n.Children))
	copy(out, n.Children)
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsFile != out[j].IsFile {
			return !out[i].IsFile
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// PrintTree 从 root 开始绘制目录树并返回统计结果。
func PrintTree(root TreeNode, opt TreeOptions) TreeStats {
	var st TreeStats
	printNode(&root, "", opt, 1, &st)
	return st
}

func printNode(n *TreeNode, prefix string, opt TreeOptions, depth int, st *TreeStats) {
	children := n.sortedChildren()
	for i := range children {
		c := &children[i]
		last := i == len(children)-1
		branch := "├── "
		nextPrefix := prefix + "│   "
		if last {
			branch = "└── "
			nextPrefix = prefix + "    "
		}

		if c.IsFile {
			if !opt.Files {
				continue
			}
			st.Files++
			st.TotalSize += c.Size
			if opt.ShowSize {
				fmtutil.PrintfGreen("%s%s%s", prefix, branch, c.Name)
				fmtutil.PrintfCyan("  [%s]\n", fmtutil.FormatBytes(c.Size))
			} else {
				fmtutil.PrintfGreen("%s%s%s\n", prefix, branch, c.Name)
			}
			continue
		}

		st.Dirs++
		fmtutil.PrintfBlue("%s%s%s/\n", prefix, branch, c.Name)
		if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
			continue
		}
		printNode(c, nextPrefix, opt, depth+1, st)
	}
}

// CountTree 只统计不渲染: 与 PrintTree 共享同一套深度/文件可见性规则,
// 保证 --json 模式下的计数与文本模式完全一致。
func CountTree(root TreeNode, opt TreeOptions) TreeStats {
	var st TreeStats
	countNode(&root, opt, 1, &st)
	return st
}

func countNode(n *TreeNode, opt TreeOptions, depth int, st *TreeStats) {
	for _, c := range n.sortedChildren() {
		if c.IsFile {
			if opt.Files {
				st.Files++
				st.TotalSize += c.Size
			}
			continue
		}
		st.Dirs++
		if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
			continue
		}
		countNode(&c, opt, depth+1, st)
	}
}
