// object-tree.go 实现对象树形展示 TreeObjects: 把扁平 key 组织成目录树,
// 支持最大深度与叶子大小展示.

package action

import (
	"errors"
	"sort"
	"strings"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/i18n"
	"s3cli/pkg/s3iface"
)

// TreeOptions tree 命令参数.
type TreeOptions struct {
	MaxDepth int  // 最大展示层级 (0 = 不限制)
	ShowSize bool // 是否在叶子上显示文件大小
	Files    bool // -f: 是否展示文件 (默认仅目录)
	JSON     bool // --json: 输出整棵树 JSON
}

func (c *Action) TreeObjects(opt TreeOptions, bucket, prefix string) error {
	if bucket == "" {
		return errors.New(i18n.T("tree requires a bucket", "tree 需要指定存储桶"))
	}

	root := &treeNode{name: "", children: map[string]*treeNode{}}
	var fileCount, dirCount int
	var totalSize int64

	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj s3iface.ObjectInfo) error {
		rel := strings.TrimPrefix(obj.Key, prefix)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		root.insert(strings.Split(rel, "/"), obj.Size)
		return nil
	})
	if err != nil {
		return err
	}

	header := c.S3Path(bucket, prefix)
	header = strings.TrimSuffix(header, "/")

	if opt.JSON {
		countTree(root, &fileCount, &dirCount, &totalSize, opt, 1)
		return printJSONLine(map[string]any{
			"path":        header,
			"directories": dirCount,
			"files":       fileCount,
			"totalSize":   totalSize,
			"tree":        root.toJSON(),
		})
	}

	myprint.Println(header)
	root.print("", opt, 1, &fileCount, &dirCount, &totalSize)

	myprint.Printf(i18n.T("\n%d directories, %d files (", "\n%d 个目录，%d 个文件（"), dirCount, fileCount)
	myprint.PrintfCyan("%s", FormatBytes(totalSize))
	myprint.Print(i18n.T(")\n", "）\n"))
	return nil
}

type treeNode struct {
	name     string
	size     int64 // only for leaf (file)
	isFile   bool
	children map[string]*treeNode
}

func (n *treeNode) insert(parts []string, size int64) {
	if len(parts) == 0 {
		return
	}
	head := parts[0]
	child, ok := n.children[head]
	if !ok {
		child = &treeNode{name: head, children: map[string]*treeNode{}}
		n.children[head] = child
	}
	if len(parts) == 1 {
		child.isFile = true
		child.size = size
		return
	}
	child.insert(parts[1:], size)
}

func (n *treeNode) sortedChildren() []*treeNode {
	out := make([]*treeNode, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		// 目录在前, 文件在后, 同类按名字
		if out[i].isFile != out[j].isFile {
			return !out[i].isFile
		}
		return out[i].name < out[j].name
	})
	return out
}

// countTree 统计树中的目录/文件数与文件总大小 (与文本模式 print 的计数口径一致:
// MaxDepth 处仍计入该层目录, 但不深入其子级)。
func countTree(n *treeNode, fileCount, dirCount *int, totalSize *int64, opt TreeOptions, depth int) {
	for _, c := range n.sortedChildren() {
		if c.isFile {
			if opt.Files {
				*fileCount++
				*totalSize += c.size
			}
			continue
		}
		*dirCount++
		if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
			continue
		}
		countTree(c, fileCount, dirCount, totalSize, opt, depth+1)
	}
}

// toJSON 把节点转为稳定的 JSON 结构:
// 目录 {"name","type":"dir","children":[...]}, 文件 {"name","type":"file","size"}。
func (n *treeNode) toJSON() map[string]any {
	m := map[string]any{"name": n.name, "type": "dir"}
	if n.isFile {
		m["type"] = "file"
		m["size"] = n.size
		return m
	}
	children := n.sortedChildren()
	childJSON := make([]map[string]any, 0, len(children))
	for _, c := range children {
		childJSON = append(childJSON, c.toJSON())
	}
	m["children"] = childJSON
	return m
}

func (n *treeNode) print(prefix string, opt TreeOptions, depth int,
	fileCount, dirCount *int, totalSize *int64) {

	children := n.sortedChildren()
	for i, c := range children {
		last := i == len(children)-1
		branch := "├── "
		nextPrefix := prefix + "│   "
		if last {
			branch = "└── "
			nextPrefix = prefix + "    "
		}

		if c.isFile {
			if !opt.Files {
				continue
			}
			*fileCount++
			*totalSize += c.size
			if opt.ShowSize {
				myprint.PrintfGreen("%s%s%s", prefix, branch, c.name)
				myprint.PrintfCyan("  [%s]\n", FormatBytes(c.size))
			} else {
				myprint.PrintfGreen("%s%s%s\n", prefix, branch, c.name)
			}
		} else {
			*dirCount++
			myprint.PrintfBlue("%s%s%s/\n", prefix, branch, c.name)
			if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
				continue
			}
			c.print(nextPrefix, opt, depth+1, fileCount, dirCount, totalSize)
		}
	}
}
