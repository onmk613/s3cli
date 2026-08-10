package api

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"s3cli/pkg/s3iface"
)

// buildSelectFrame 按 S3 Select 事件流帧格式构造一条消息 (与 minio-go/mc 一致):
// prelude(totalLen, headerLen) + preludeCRC + headers + payload + messageCRC.
// 消息 CRC 覆盖整个消息体 (prelude + preludeCRC + headers + payload).
func buildSelectFrame(t *testing.T, headerKVs map[string]string, payload []byte) []byte {
	t.Helper()
	var headerBytes []byte
	for name, val := range headerKVs {
		headerBytes = append(headerBytes, byte(len(name)))
		headerBytes = append(headerBytes, name...)
		headerBytes = append(headerBytes, 7) // string
		var vlen [2]byte
		binary.BigEndian.PutUint16(vlen[:], uint16(len(val)))
		headerBytes = append(headerBytes, vlen[:]...)
		headerBytes = append(headerBytes, val...)
	}

	prelude := make([]byte, 8)
	binary.BigEndian.PutUint32(prelude[0:4], uint32(12+len(headerBytes)+len(payload)+4))
	binary.BigEndian.PutUint32(prelude[4:8], uint32(len(headerBytes)))

	var frame []byte
	frame = append(frame, prelude...)
	var preCRC [4]byte
	binary.BigEndian.PutUint32(preCRC[:], crc32.ChecksumIEEE(prelude))
	frame = append(frame, preCRC[:]...)
	frame = append(frame, headerBytes...)
	frame = append(frame, payload...)

	crc := crc32.New(crc32.IEEETable)
	_, _ = crc.Write(frame)
	var msgCRC [4]byte
	binary.BigEndian.PutUint32(msgCRC[:], crc.Sum32())
	frame = append(frame, msgCRC[:]...)
	return frame
}

func TestParseSelectStream(t *testing.T) {
	event := func(name string, payload []byte) []byte {
		return buildSelectFrame(t, map[string]string{
			":message-type": "event",
			":event-type":   name,
			":content-type": "application/xml",
		}, payload)
	}

	var frames []byte
	frames = append(frames, event("Records", []byte("alice,30,beijing\n"))...)
	frames = append(frames, event("Records", []byte("bob,25,shanghai\n"))...)
	frames = append(frames, event("Stats", []byte(`<?xml version="1.0" encoding="UTF-8"?><Stats><BytesScanned>66</BytesScanned><BytesProcessed>66</BytesProcessed><BytesReturned>36</BytesReturned></Stats>`))...)
	frames = append(frames, event("End", nil)...)

	var got []string
	stats, err := parseSelectStream(t.Context(), bytes.NewReader(frames), func(payload []byte) error {
		got = append(got, string(payload))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "alice,30,beijing\n" || got[1] != "bob,25,shanghai\n" {
		t.Errorf("records = %v", got)
	}
	if stats.BytesScanned != 66 || stats.BytesProcessed != 66 || stats.BytesReturned != 36 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestParseSelectStreamErrorEvent(t *testing.T) {
	frame := buildSelectFrame(t, map[string]string{
		":message-type":  "error",
		":error-code":    "InternalError",
		":error-message": "boom",
	}, nil)
	if _, err := parseSelectStream(t.Context(), bytes.NewReader(frame), nil); err == nil {
		t.Fatal("expected error event to fail")
	}
}

func TestParseSelectStreamCorruptCRC(t *testing.T) {
	frame := buildSelectFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "Records",
	}, []byte("x"))
	// 破坏最后一个字节 (message CRC)
	frame[len(frame)-1] ^= 0xff
	if _, err := parseSelectStream(t.Context(), bytes.NewReader(frame), nil); err == nil {
		t.Fatal("expected crc mismatch error")
	}
}

func TestParseSelectHeaders(t *testing.T) {
	headers := parseSelectHeaders([]byte{
		0x0d, ':', 'm', 'e', 's', 's', 'a', 'g', 'e', '-', 't', 'y', 'p', 'e',
		7, 0x00, 0x05, 'e', 'v', 'e', 'n', 't',
		0x0b, ':', 'e', 'v', 'e', 'n', 't', '-', 't', 'y', 'p', 'e',
		7, 0x00, 0x07, 'R', 'e', 'c', 'o', 'r', 'd', 's',
	})
	if headers["message-type"] != "event" || headers["event-type"] != "Records" {
		t.Errorf("headers = %v", headers)
	}
}

func TestBuildSelectXML(t *testing.T) {
	body, err := buildSelectXML(&s3iface.SelectObjectInput{
		Expression: "select * from S3Object",
		InputSerialization: &s3iface.SelectSerialization{
			Format: "CSV", CompressionType: "GZIP", FileHeaderInfo: "USE",
			FieldDelimiter: ";", RecordDelimiter: "\n",
		},
		OutputSerialization: &s3iface.SelectSerialization{Format: "CSV", RecordDelimiter: "\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"SelectObjectContentRequest", "select * from S3Object", "ExpressionType",
		"CompressionType", "GZIP", "FileHeaderInfo", "USE", "FieldDelimiter", ";",
		"OutputSerialization", "RecordDelimiter",
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Errorf("xml missing %q: %s", want, s)
		}
	}

	// 未指定 CSV 选项时不输出 CSV 元素
	body, err = buildSelectXML(&s3iface.SelectObjectInput{
		Expression:         "select * from S3Object",
		InputSerialization: &s3iface.SelectSerialization{Format: "CSV", CompressionType: "NONE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("<CSV>")) {
		t.Errorf("should not emit empty CSV element: %s", body)
	}

	// Parquet 输入
	body, err = buildSelectXML(&s3iface.SelectObjectInput{
		Expression:         "select * from S3Object",
		InputSerialization: &s3iface.SelectSerialization{Format: "PARQUET", CompressionType: "NONE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("<Parquet>")) {
		t.Errorf("missing Parquet element: %s", body)
	}
}
