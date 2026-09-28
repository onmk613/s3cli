// object-restore.go 实现 RestoreObject (POST ?restore): 请求从归档存储类
// (GLACIER / DEEP_ARCHIVE 等) 恢复一个对象的可访问副本.
// 服务端接受请求返回 202 Accepted (已发起恢复) 或 200 OK (已是可访问);
// 二者均为 2xx, 由 Do 视为成功.

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
)

// RestoreObject 请求恢复归档对象. req.Days 为恢复后保持可访问的天数,
// req.GlacierJobParameters.Tier 为恢复层级 (Expedited/Standard/Bulk).
func (c *Client) RestoreObject(ctx context.Context, bucket, key, versionID string, req *RestoreRequest) error {
	if req == nil {
		req = &RestoreRequest{Days: 1}
	}
	if req.Days <= 0 {
		req.Days = 1
	}
	body, err := marshalXMLWithHeader(req)
	if err != nil {
		return err
	}

	q := make(url.Values)
	q.Set("restore", "")
	if versionID != "" {
		q.Set("versionId", versionID)
	}

	reqMeta := requestMetadata{
		bucketName:       bucket,
		objectName:       key,
		queryValues:      q,
		contentBody:      bytes.NewReader(body),
		contentLength:    int64(len(body)),
		contentMD5Base64: sumMD5Base64(body),
		contentSHA256Hex: sumSHA256Hex(body),
		customHeader:     http.Header{"Content-Type": []string{"application/xml"}},
	}

	resp, err := c.Do(ctx, http.MethodPost, reqMeta)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	return nil
}
