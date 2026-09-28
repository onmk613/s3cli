package action

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResumeRefusesFileWithSameSizeAndMtimeButNewContent 是数据完整性回归测试。
//
// 修复前状态文件的"文件指纹"只有 size + mtime。rsync -t、tar -p、touch -r、
// 还原构建产物都能造出"size 与 mtime 完全一致但内容不同"的文件 —— 此时续传
// 会复用服务端已存在的旧分片, 只补传剩余部分, 最终完成的对象是"旧内容 + 新内容"
// 的拼接体, 且全程没有任何报错。这是最隐蔽的一类静默数据损坏。
func TestResumeRefusesFileWithSameSizeAndMtimeButNewContent(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t) // 内容 "0123456789"
	fi, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	// 基于"原始内容"写入一份状态文件 (含正确指纹)。
	statePath := writeResumeState(t, local, "stale-uid", fi)

	// 原地改写内容, 但把 mtime 复原 —— size 与 mtime 都与状态文件一致。
	if err := os.WriteFile(local, []byte("9876543210"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(local, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != fi.Size() {
		t.Fatalf("test precondition: size changed (%d -> %d)", fi.Size(), after.Size())
	}
	if !after.ModTime().Equal(fi.ModTime()) {
		t.Fatalf("test precondition: mtime changed (%v -> %v)", fi.ModTime(), after.ModTime())
	}

	s := newMpuResumeServer(t)
	defer s.srv.Close()
	s.mu.Lock()
	// 服务端声称已有第 1 片 (来自旧内容)。若实现仍然续传, 这一片会被直接复用。
	s.listPartsXML = `<ListPartsResult><Bucket>mybucket</Bucket><Key>key.bin</Key><UploadId>stale-uid</UploadId><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>etag-1</ETag></Part></ListPartsResult>`
	s.mu.Unlock()

	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadPartCount != 1 {
		t.Fatalf("uploadParts=%d, want 1: the whole file must be re-uploaded, "+
			"never resumed against parts holding different bytes", s.uploadPartCount)
	}
	if s.completeUID == "stale-uid" {
		t.Fatal("completed against the stale upload id: old parts were reused for new content (silent corruption)")
	}
	if s.abortCount != 1 {
		t.Fatalf("abortCount=%d, want 1: the stale upload must be aborted server-side "+
			"instead of leaking orphaned parts", s.abortCount)
	}
	if s.createCount != 1 {
		t.Fatalf("createCount=%d, want 1: a fresh upload must be created", s.createCount)
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state file should be removed after success (err=%v)", err)
	}
}

// TestResumeStillWorksWhenContentIsUnchanged 对照组: 文件没变时仍然续传,
// 确保上面的防护不是"一律拒绝续传"。
func TestResumeStillWorksWhenContentIsUnchanged(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	fi, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	writeResumeState(t, local, "resume-uid", fi)

	s := newMpuResumeServer(t)
	defer s.srv.Close()
	s.mu.Lock()
	s.listPartsXML = `<ListPartsResult><Bucket>mybucket</Bucket><Key>key.bin</Key><UploadId>resume-uid</UploadId><IsTruncated>false</IsTruncated><Part><PartNumber>1</PartNumber><ETag>etag-1</ETag></Part></ListPartsResult>`
	s.mu.Unlock()

	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.uploadPartCount != 0 {
		t.Fatalf("uploadParts=%d, want 0 (unchanged file must reuse the existing part)", s.uploadPartCount)
	}
	if s.completeUID != "resume-uid" {
		t.Fatalf("completed with %q, want resume-uid", s.completeUID)
	}
	if s.abortCount != 0 {
		t.Fatalf("abortCount=%d, want 0", s.abortCount)
	}
}

// TestLegacyStateWithoutDigestIsNotResumed 覆盖升级路径: 旧版本写下的状态文件
// 没有 content_digest 字段, 必须安全地重建上传, 而不是盲目信任 size+mtime。
func TestLegacyStateWithoutDigestIsNotResumed(t *testing.T) {
	setupMpuHome(t)
	local := smallLocalFile(t)
	fi, err := os.Stat(local)
	if err != nil {
		t.Fatal(err)
	}
	statePath, err := multipartStatePath(local, "mybucket", "key.bin")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := multipartState{
		Version: 1, UploadID: "legacy-uid", Bucket: "mybucket", Key: "key.bin",
		LocalPath: local, PartSize: minMultipartPartSize,
		TotalSize: fi.Size(), ModTimeUnixNs: fi.ModTime().UnixNano(),
		// 刻意不写 ContentDigest —— 模拟旧版本落下的状态文件。
	}
	if err := os.WriteFile(statePath, mustJSON(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMpuResumeServer(t)
	defer s.srv.Close()
	if err := runResumeUpload(t, s, local); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.completeUID == "legacy-uid" {
		t.Fatal("a digest-less legacy state must not be resumed")
	}
	if s.abortCount != 1 {
		t.Fatalf("abortCount=%d, want 1 (legacy upload must be aborted, not leaked)", s.abortCount)
	}
}
