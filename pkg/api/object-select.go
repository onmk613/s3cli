// object-select.go 实现 S3 Select (SelectObjectContent) 的自建客户端.
//
// 请求: POST /bucket/key?select=&select-type=2, body 为 SelectObjectContentRequest XML.
// 响应: 事件流 (event stream), 每个消息的帧格式:
//
//	prelude (12 字节): TotalLength(4, BE) | HeadersLength(4, BE) | PreludeCRC(4, BE, CRC32-IEEE)
//	headers (HeadersLength 字节): 每项 = NameLen(1) | Name | ValueType(1, 7=string) | ValueLen(2, BE) | Value
//	payload (变长)
//	message CRC (4, BE, CRC32-IEEE, 覆盖 headers+payload)
//
// header 中 ":message-type" 为 event/error, "event-type" 为 Records/Progress/Stats/End/Continuation.

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strings"

	"s3cli/pkg/s3iface"
)

// ----------------------------------------------------------------------------
// 请求 XML
// ----------------------------------------------------------------------------

type selectCSVInput struct {
	FileHeaderInfo       string `xml:"FileHeaderInfo,omitempty"`
	FieldDelimiter       string `xml:"FieldDelimiter,omitempty"`
	RecordDelimiter      string `xml:"RecordDelimiter,omitempty"`
	QuoteCharacter       string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter,omitempty"`
	Comments             string `xml:"Comments,omitempty"`
}

type selectCSVOutput struct {
	RecordDelimiter      string `xml:"RecordDelimiter,omitempty"`
	FieldDelimiter       string `xml:"FieldDelimiter,omitempty"`
	QuoteCharacter       string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter,omitempty"`
	QuoteFields          string `xml:"QuoteFields,omitempty"`
}

type selectJSONInput struct {
	Type string `xml:"Type,omitempty"`
}

type selectJSONOutput struct {
	RecordDelimiter string `xml:"RecordDelimiter,omitempty"`
}

type selectInputSerialization struct {
	XMLName         xml.Name         `xml:"InputSerialization"`
	CompressionType string           `xml:"CompressionType,omitempty"`
	CSV             *selectCSVInput  `xml:"CSV,omitempty"`
	JSON            *selectJSONInput `xml:"JSON,omitempty"`
	Parquet         *struct{}        `xml:"Parquet,omitempty"`
}

type selectOutputSerialization struct {
	XMLName xml.Name          `xml:"OutputSerialization"`
	CSV     *selectCSVOutput  `xml:"CSV,omitempty"`
	JSON    *selectJSONOutput `xml:"JSON,omitempty"`
}

type selectRequestProgress struct {
	Enabled bool `xml:"Enabled"`
}

type selectRequest struct {
	XMLName             xml.Name                   `xml:"SelectObjectContentRequest"`
	Expression          string                     `xml:"Expression"`
	ExpressionType      string                     `xml:"ExpressionType"`
	InputSerialization  *selectInputSerialization  `xml:"InputSerialization"`
	OutputSerialization *selectOutputSerialization `xml:"OutputSerialization"`
	RequestProgress     *selectRequestProgress     `xml:"RequestProgress,omitempty"`
}

// buildSelectXML 按输入选项构造请求 XML.
func buildSelectXML(input *s3iface.SelectObjectInput) ([]byte, error) {
	req := &selectRequest{
		Expression:     input.Expression,
		ExpressionType: "SQL",
	}

	if input.InputSerialization != nil {
		ins := &selectInputSerialization{
			CompressionType: strings.ToUpper(input.InputSerialization.CompressionType),
		}
		switch strings.ToUpper(input.InputSerialization.Format) {
		case "CSV":
			// 仅当显式设置了任意 CSV 选项时才输出 CSV 元素;
			// 全空时留空 (服务端默认 CSV + 首行作表头).
			csv := &selectCSVInput{
				FileHeaderInfo:       input.InputSerialization.FileHeaderInfo,
				FieldDelimiter:       input.InputSerialization.FieldDelimiter,
				RecordDelimiter:      input.InputSerialization.RecordDelimiter,
				QuoteCharacter:       input.InputSerialization.QuoteCharacter,
				QuoteEscapeCharacter: input.InputSerialization.QuoteEscapeCharacter,
				Comments:             input.InputSerialization.CommentCharacter,
			}
			if csv.FileHeaderInfo != "" || csv.FieldDelimiter != "" || csv.RecordDelimiter != "" ||
				csv.QuoteCharacter != "" || csv.QuoteEscapeCharacter != "" || csv.Comments != "" {
				ins.CSV = csv
			}
		case "JSON":
			if input.InputSerialization.JSONType != "" {
				ins.JSON = &selectJSONInput{Type: input.InputSerialization.JSONType}
			}
		case "PARQUET":
			ins.Parquet = &struct{}{}
		}
		req.InputSerialization = ins
	}

	if input.OutputSerialization != nil {
		outs := &selectOutputSerialization{}
		switch strings.ToUpper(input.OutputSerialization.Format) {
		case "JSON":
			outs.JSON = &selectJSONOutput{RecordDelimiter: input.OutputSerialization.RecordDelimiter}
		default: // CSV (默认)
			outs.CSV = &selectCSVOutput{
				RecordDelimiter:      input.OutputSerialization.RecordDelimiter,
				FieldDelimiter:       input.OutputSerialization.FieldDelimiter,
				QuoteCharacter:       input.OutputSerialization.QuoteCharacter,
				QuoteEscapeCharacter: input.OutputSerialization.QuoteEscapeCharacter,
				QuoteFields:          strings.ToUpper(input.OutputSerialization.QuoteFields),
			}
		}
		req.OutputSerialization = outs
	}

	if input.RequestProgress {
		req.RequestProgress = &selectRequestProgress{Enabled: true}
	}

	return xml.Marshal(req)
}

// ----------------------------------------------------------------------------
// 事件流解析
// ----------------------------------------------------------------------------

// SelectObjectContent 对对象执行 SQL 查询, 每条 Records 事件回调 onRecord.
func (c *Client) SelectObjectContent(ctx context.Context, bucket, key string, input *s3iface.SelectObjectInput, onRecord func(payload []byte) error) (*s3iface.SelectObjectStats, error) {
	if input == nil {
		input = &s3iface.SelectObjectInput{Expression: "select * from S3Object"}
	}
	body, err := buildSelectXML(input)
	if err != nil {
		return nil, fmt.Errorf("build select request: %w", err)
	}

	urlValues := make(url.Values)
	urlValues.Set("select", "")
	urlValues.Set("select-type", "2")

	resp, err := c.Do(ctx, http.MethodPost, requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      urlValues,
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader: http.Header{
			"Content-Type": []string{"application/xml"},
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return parseSelectStream(ctx, resp.Body, onRecord)
}

// parseSelectStream 解析事件流, 直到 End 事件.
func parseSelectStream(ctx context.Context, body io.Reader, onRecord func(payload []byte) error) (*s3iface.SelectObjectStats, error) {
	br := bufio.NewReader(body)
	stats := &s3iface.SelectObjectStats{}

	for {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}
		// prelude: TotalLength(4) + HeadersLength(4)
		var prelude [8]byte
		if _, err := io.ReadFull(br, prelude[:]); err != nil {
			if err == io.EOF {
				break // 流正常结束
			}
			return stats, fmt.Errorf("read select event prelude: %w", err)
		}
		totalLen := binary.BigEndian.Uint32(prelude[0:4])
		headerLen := binary.BigEndian.Uint32(prelude[4:8])

		// prelude CRC (CRC32-IEEE, 覆盖前 8 字节)
		var preCRC [4]byte
		if _, err := io.ReadFull(br, preCRC[:]); err != nil {
			return stats, fmt.Errorf("read prelude crc: %w", err)
		}

		if crc32.ChecksumIEEE(prelude[:]) != binary.BigEndian.Uint32(preCRC[:]) {
			return stats, fmt.Errorf("select event prelude crc mismatch")
		}
		if totalLen < 12+headerLen+4 {
			return stats, fmt.Errorf("invalid select event length %d (headers %d)", totalLen, headerLen)
		}
		payloadLen := int(totalLen) - 12 - int(headerLen) - 4

		// headers
		headerBytes := make([]byte, headerLen)
		if _, err := io.ReadFull(br, headerBytes); err != nil {
			return stats, fmt.Errorf("read select event headers: %w", err)
		}
		headers := parseSelectHeaders(headerBytes)

		// payload
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(br, payload); err != nil {
			return stats, fmt.Errorf("read select event payload: %w", err)
		}

		// message CRC: 覆盖整条消息 (prelude + preludeCRC + headers + payload)
		var msgCRC [4]byte
		if _, err := io.ReadFull(br, msgCRC[:]); err != nil {
			return stats, fmt.Errorf("read message crc: %w", err)
		}
		crc := crc32.New(crc32.IEEETable)
		_, _ = crc.Write(prelude[:])
		_, _ = crc.Write(preCRC[:])
		_, _ = crc.Write(headerBytes)
		_, _ = crc.Write(payload)
		if crc.Sum32() != binary.BigEndian.Uint32(msgCRC[:]) {
			return stats, fmt.Errorf("select event message crc mismatch")
		}

		// 分发
		switch headers["message-type"] {
		case "error":
			code := headers["error-code"]
			msg := headers["error-message"]
			if msg == "" {
				msg = string(payload)
			}
			return stats, fmt.Errorf("select error %s: %s", code, msg)
		}
		switch headers["event-type"] {
		case "Records":
			if onRecord != nil {
				if err := onRecord(payload); err != nil {
					return stats, err
				}
			}
		case "Stats":
			var sm struct {
				BytesScanned   int64 `xml:"BytesScanned"`
				BytesProcessed int64 `xml:"BytesProcessed"`
				BytesReturned  int64 `xml:"BytesReturned"`
			}
			if err := xml.Unmarshal(payload, &sm); err == nil {
				stats.BytesScanned = sm.BytesScanned
				stats.BytesProcessed = sm.BytesProcessed
				stats.BytesReturned = sm.BytesReturned
			}
		case "Progress":
			// 忽略进度事件
		case "End":
			return stats, nil
		}
	}
	return stats, nil
}

// parseSelectHeaders 解析 header 块: NameLen(1) Name ValueType(1) ValueLen(2) Value.
// 名称以 ":" 开头, 解析后去掉前缀 (如 ":message-type" -> "message-type").
func parseSelectHeaders(b []byte) map[string]string {
	out := make(map[string]string)
	for i := 0; i < len(b); {
		if i+1 > len(b) {
			break
		}
		nameLen := int(b[i])
		i++
		if i+nameLen > len(b) {
			break
		}
		name := string(b[i : i+nameLen])
		i += nameLen
		if i >= len(b) {
			break
		}
		// ValueType (1 字节) + ValueLen (2 字节 BE)
		if i+3 > len(b) {
			break
		}
		valueLen := int(binary.BigEndian.Uint16(b[i+1 : i+3]))
		i += 3
		if i+valueLen > len(b) {
			break
		}
		out[strings.TrimPrefix(name, ":")] = string(b[i : i+valueLen])
		i += valueLen
	}
	return out
}
