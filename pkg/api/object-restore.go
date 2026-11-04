package api

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
)

// RestoreRequest POST ?restore 请求体.
type RestoreRequest struct {
	XMLName              xml.Name              `xml:"RestoreRequest" json:"-"`
	Days                 int                   `xml:"Days" json:"Days"`
	GlacierJobParameters *GlacierJobParameters `xml:"GlacierJobParameters,omitempty" json:"GlacierJobParameters,omitempty"`
}

// GlacierJobParameters 归档恢复层级.
type GlacierJobParameters struct {
	Tier string `xml:"Tier" json:"Tier"` // Expedited / Standard / Bulk
}

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
