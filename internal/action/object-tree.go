// object-tree.go 实现对象树形展示 TreeObjects: 把扁平 key 组织成目录树,
// 支持最大深度与叶子大小展示.

package action

import (
	"errors"
	"s3cli/internal/action/render"
	"sort"
	"strings"

	"s3cli/internal/api"
	myprint "s3cli/internal/fmtutil"
	"s3cli/internal/i18n"
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

	err := c.forEachObject(c.Ctx, bucket, prefix, func(obj api.ObjectInfo) error {
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

	view := root.view()
	renOpt := render.TreeOptions{Files: opt.Files, ShowSize: opt.ShowSize, MaxDepth: opt.MaxDepth}

	if opt.JSON {
		st := render.CountTree(view, renOpt)
		return render.JSONLine(map[string]any{
			"path":        header,
			"directories": st.Dirs,
			"files":       st.Files,
			"totalSize":   st.TotalSize,
			"tree":        root.toJSON(),
		})
	}

	myprint.Println(header)
	st := render.PrintTree(view, renOpt)

	myprint.Printf(i18n.T("\n%d directories, %d files (", "\n%d 个目录，%d 个文件（"), st.Dirs, st.Files)
	myprint.PrintfCyan("%s", myprint.FormatBytes(st.TotalSize))
	myprint.Print(i18n.T(")\n", "）\n"))
	return nil
}

// view 把内部树结构转换为渲染层的最小视图 (渲染层不认识 treeNode)。
func (n *treeNode) view() render.TreeNode {
	children := n.sortedChildren()
	out := make([]render.TreeNode, 0, len(children))
	for _, c := range children {
		out = append(out, c.view())
	}
	return render.TreeNode{Name: n.name, IsFile: n.isFile, Size: n.size, Children: out}
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
