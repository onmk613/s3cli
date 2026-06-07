package action

import (
	"fmt"
	"sort"
	"strings"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TreeOptions tree 命令参数
type TreeOptions struct {
	MaxDepth int  // 最大展示层级 (0 = 不限制)
	ShowSize bool // 是否在叶子上显示文件大小
}

// TreeObjects 把 s3://bucket/prefix 下的对象按目录结构以树形输出
func (c *S3Client) TreeObjects(opt TreeOptions, bucket, prefix string) error {
	if bucket == "" {
		return fmt.Errorf("tree requires a bucket")
	}

	root := &treeNode{name: "", children: map[string]*treeNode{}}
	var fileCount, dirCount int
	var totalSize int64

	paginator := s3.NewListObjectsV2Paginator(c.S3, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(c.Ctx)
		if err != nil {
			return fmt.Errorf("list objects: %s", FormatAPIError(err))
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			rel := strings.TrimPrefix(key, prefix)
			rel = strings.TrimPrefix(rel, "/")
			if rel == "" {
				continue
			}
			root.insert(strings.Split(rel, "/"), aws.ToInt64(obj.Size))
		}
	}

	header := c.S3Path(bucket, prefix)
	header = strings.TrimSuffix(header, "/")
	myprint.Println(header)
	root.print("", opt, 1, &fileCount, &dirCount, &totalSize)

	myprint.Printf("\n%d directories, %d files (", dirCount, fileCount)
	myprint.PrintfCyan("%s", FormatBytes(totalSize))
	myprint.Printf(")\n")
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
