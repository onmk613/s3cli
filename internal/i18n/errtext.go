// errtext.go 在 CLI 输出边界本地化"错误上下文前缀"。
//
// 内部错误链为保持可测试性与日志稳定性, 统一用英文上下文
// (如 "check s3 path: ...") 包装; 直接把这些英文前缀混进中文界面
// 会得到 "check s3 path: 路径 ... 不存在" 这类中英混排。
//
// 与其在两百多处 fmt.Errorf 里各写一遍 i18n.T (漏一处就混排), 不如在
// 展示层做一次受控翻译: 只匹配本代码库自己产生的上下文前缀, 保留
// %w 错误链、API 返回的英文原文与未知前缀不变。新增前缀时在此补一条规则,
// errtext_test.go 会锁定已知规则。
package i18n

import "regexp"

type errTextRule struct {
	re *regexp.Regexp
	zh string // 支持 $1/$2 展开
}

// 顺序重要: 更具体的规则必须排在通用规则之前。
var errTextRules = []errTextRule{
	{regexp.MustCompile(`^check destination \(treated as not-exist\): `), "检查目标（按不存在处理）："},
	{regexp.MustCompile(`^check s3 path: `), "检查 S3 路径："},
	{regexp.MustCompile(`^check source: `), "检查源："},
	{regexp.MustCompile(`^check destination: `), "检查目标："},
	{regexp.MustCompile(`^check existing object: `), "检查目标对象是否存在："},
	{regexp.MustCompile(`^list objects: `), "列出对象："},
	{regexp.MustCompile(`^list buckets: `), "列出存储桶："},
	{regexp.MustCompile(`^list multipart uploads: `), "列出进行中的分段上传："},
	{regexp.MustCompile(`^list mpu: `), "列出分段上传："},
	{regexp.MustCompile(`^list (.+?): `), "列出 $1："},
	{regexp.MustCompile(`^head object: `), "读取对象元信息："},
	{regexp.MustCompile(`^head (.+?): `), "读取 $1 元信息："},
	{regexp.MustCompile(`^stat path: `), "读取本地路径："},
	{regexp.MustCompile(`^stat (.+?): `), "读取 $1 信息："},
	{regexp.MustCompile(`^copy (.+?): `), "复制 $1："},
	{regexp.MustCompile(`^delete batch of (\d+) from (.+?): `), "删除 $2 中的 $1 个对象："},
	{regexp.MustCompile(`^delete source: `), "删除源对象："},
	{regexp.MustCompile(`^delete (.+?): `), "删除 $1："},
	{regexp.MustCompile(`^download: `), "下载："},
	{regexp.MustCompile(`^upload: `), "上传："},
	{regexp.MustCompile(`^write file: `), "写入文件："},
	{regexp.MustCompile(`^write stdout: `), "写入 stdout："},
	{regexp.MustCompile(`^write part: `), "写入分片："},
	{regexp.MustCompile(`^create file: `), "创建文件："},
	{regexp.MustCompile(`^create spool file: `), "创建临时文件："},
	{regexp.MustCompile(`^create delete spool: `), "创建删除清单："},
	{regexp.MustCompile(`^write delete spool: `), "写入删除清单："},
	{regexp.MustCompile(`^flush delete spool: `), "刷新删除清单："},
	{regexp.MustCompile(`^rewind delete spool: `), "回卷删除清单："},
	{regexp.MustCompile(`^read delete spool: `), "读取删除清单："},
	{regexp.MustCompile(`^create multipart upload: `), "创建分段上传："},
	{regexp.MustCompile(`^replace file: `), "替换文件："},
	{regexp.MustCompile(`^mkdir: `), "创建目录："},
	{regexp.MustCompile(`^read stdin: `), "读取 stdin："},
	{regexp.MustCompile(`^read s3: `), "读取 S3："},
	{regexp.MustCompile(`^read part (\d+): `), "读取分片 $1："},
	{regexp.MustCompile(`^read multipart part (\d+): `), "读取分段分片 $1："},
	{regexp.MustCompile(`^upload multipart part (\d+): `), "上传分段分片 $1："},
	{regexp.MustCompile(`^upload part (\d+): `), "上传分片 $1："},
	{regexp.MustCompile(`^upload (.+?): `), "上传 $1："},
	{regexp.MustCompile(`^complete multipart upload: `), "完成分段上传："},
	{regexp.MustCompile(`^rewind spool file: `), "回卷临时文件："},
	{regexp.MustCompile(`^parse (.+?): `), "解析 $1："},
	{regexp.MustCompile(`^get bucket policy (.+?): `), "获取存储桶 $1 的策略："},
	{regexp.MustCompile(`^get bucket versioning (.+?): `), "获取存储桶 $1 的版本控制："},
	{regexp.MustCompile(`^get bucket location: `), "获取存储桶区域："},
	{regexp.MustCompile(`^get bucket cors (.+?): `), "获取存储桶 $1 的 CORS："},
	{regexp.MustCompile(`^get bucket tag: `), "获取存储桶标签："},
	{regexp.MustCompile(`^get object tag: `), "获取对象标签："},
	{regexp.MustCompile(`^get lifecycle (.+?): `), "获取存储桶 $1 的生命周期："},
	{regexp.MustCompile(`^get (.+?): `), "获取 $1："},
	{regexp.MustCompile(`^set lifecycle rule (.+?): `), "设置存储桶 $1 的生命周期规则："},
	{regexp.MustCompile(`^set cors (.+?): `), "设置存储桶 $1 的 CORS："},
	{regexp.MustCompile(`^set encryption (.+?): `), "设置存储桶 $1 的加密："},
	{regexp.MustCompile(`^set replication (.+?): `), "设置存储桶 $1 的复制："},
	{regexp.MustCompile(`^set public access block (.+?): `), "设置存储桶 $1 的公共访问阻断："},
	{regexp.MustCompile(`^set notification (.+?): `), "设置存储桶 $1 的事件通知："},
	{regexp.MustCompile(`^set versioning (.+?): `), "设置存储桶 $1 的版本控制："},
	{regexp.MustCompile(`^set object acl (.+?): `), "设置对象 $1 的 ACL："},
	{regexp.MustCompile(`^set bucket acl (.+?): `), "设置存储桶 $1 的 ACL："},
	{regexp.MustCompile(`^set bucket tag: `), "设置存储桶标签："},
	{regexp.MustCompile(`^set object tag: `), "设置对象标签："},
	{regexp.MustCompile(`^set (.+?): `), "设置 $1："},
	{regexp.MustCompile(`^remove lifecycle rule (.+?): `), "移除生命周期规则 $1："},
	{regexp.MustCompile(`^remove lifecycle (.+?): `), "移除存储桶 $1 的生命周期："},
	{regexp.MustCompile(`^remove (.+?): `), "移除 $1："},
	{regexp.MustCompile(`^restore (.+?): `), "恢复 $1："},
	{regexp.MustCompile(`^sql (.+?): `), "查询 $1："},
	{regexp.MustCompile(`^save config: `), "保存配置："},
	{regexp.MustCompile(`^save multipart state: `), "保存分段上传状态："},
	{regexp.MustCompile(`^seek resumable multipart upload: `), "定位可恢复的分段上传："},
	{regexp.MustCompile(`^resolve multipart state path: `), "解析分段上传状态路径："},
	{regexp.MustCompile(`^read input: `), "读取输入："},
	{regexp.MustCompile(`^empty s3 path$`), "S3 路径为空"},
}

// LocalizeErrorText 在中文界面下翻译内部错误上下文前缀。
// 英文界面、未知前缀、API 原始错误文本均原样返回。
func LocalizeErrorText(s string) string {
	if Current() != Zh {
		return s
	}
	return localizePrefix(s)
}

func localizePrefix(s string) string {
	for _, rule := range errTextRules {
		loc := rule.re.FindStringSubmatchIndex(s)
		if loc == nil || loc[0] != 0 {
			continue
		}
		prefix := rule.re.ExpandString(nil, rule.zh, s, loc)
		rest := s[loc[1]:]
		return string(prefix) + localizePrefix(rest)
	}
	return s
}
