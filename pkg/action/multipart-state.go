package action

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// multipartState persists only the information required to safely reconnect a
// local file to an existing S3 multipart upload. Completed parts are always
// re-read from S3 before resuming; the state file is never authoritative.
type multipartState struct {
	Version       int    `json:"version"`
	UploadID      string `json:"upload_id"`
	Bucket        string `json:"bucket"`
	Key           string `json:"key"`
	LocalPath     string `json:"local_path"`
	PartSize      int64  `json:"part_size"`
	TotalSize     int64  `json:"total_size"`
	ModTimeUnixNs int64  `json:"mod_time_unix_ns"`
	CreatedAt     string `json:"created_at"`
}

type LocalMultipartState struct {
	UploadID  string `json:"upload_id"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	LocalPath string `json:"local_path"`
	TotalSize int64  `json:"total_size"`
	CreatedAt string `json:"created_at"`
	StatePath string `json:"state_path"`
}

func localMultipartStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".s3cli", "mpu"), nil
}

func ListLocalMultipartStates() ([]LocalMultipartState, error) {
	dir, err := localMultipartStateDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []LocalMultipartState{}, nil
	}
	if err != nil {
		return nil, err
	}
	states := make([]LocalMultipartState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var state multipartState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		states = append(states, LocalMultipartState{UploadID: state.UploadID, Bucket: state.Bucket, Key: state.Key, LocalPath: state.LocalPath, TotalSize: state.TotalSize, CreatedAt: state.CreatedAt, StatePath: path})
	}
	return states, nil
}

func ClearLocalMultipartState(path string) error {
	dir, err := localMultipartStateDir()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if filepath.Dir(abs) != dir || filepath.Ext(abs) != ".json" {
		return fmt.Errorf("refusing to remove non-state file %q", path)
	}
	return os.Remove(abs)
}

func multipartStatePath(localPath, bucket, key string) (string, error) {
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs + "\x00" + bucket + "\x00" + key))
	dir := filepath.Join(home, ".s3cli", "mpu")
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}

func loadMultipartState(localPath, bucket, key string, size int64, modTime time.Time) (*multipartState, string, error) {
	path, err := multipartStatePath(localPath, bucket, key)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	var state multipartState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, path, fmt.Errorf("decode multipart state: %w", err)
	}
	if state.Version != 1 || state.UploadID == "" || state.Bucket != bucket || state.Key != key || state.TotalSize != size || state.ModTimeUnixNs != modTime.UnixNano() {
		return nil, path, nil
	}
	return &state, path, nil
}

func saveMultipartState(path string, state multipartState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mpu-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func(name string) {
		_ = os.Remove(name)
	}(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
