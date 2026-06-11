package action

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	myprint "s3cli/pkg/fmtutil"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// SignurlOpt signurl 参数
type SignurlOptions struct {
	ExpireSeconds int
	Method        string // GET / PUT / DELETE / HEAD
	SignurlV2     bool
}

// Signurl 生成预签名 URL
func (c *S3Client) Signurl(opt SignurlOptions, bucketname, key string) error {
	method := strings.ToUpper(strings.TrimSpace(opt.Method))
	if method == "" {
		method = "GET"
	}

	if method == "GET" || method == "HEAD" {
		ok, err := c.IsS3File(bucketname, key)
		if err != nil {
			return fmt.Errorf("check s3 path: %s", FormatAPIError(err))
		}
		if !ok {
			return fmt.Errorf("%s: not a file", c.S3Path(bucketname, key))
		}
	}

	cred, err := c.GetCreds()
	if err != nil {
		return fmt.Errorf("get credentials: %s", FormatAPIError(err))
	}

	var signed string
	if opt.SignurlV2 {
		signed = urlSignV2(cred.AccessKeyID, cred.SecretAccessKey, bucketname, cred.BaseEndpoint, key, opt.ExpireSeconds)
	} else {
		if opt.ExpireSeconds > 604800 {
			myprint.PrintfYellow("Warning: v4 signature maximum validity is 7 days (604800s), the generated URL may expire earlier\n")
		}
		var perr error
		signed, perr = c.presignV4(method, bucketname, key, opt.ExpireSeconds)
		if perr != nil {
			return fmt.Errorf("presign: %s", FormatAPIError(perr))
		}
	}

	myprint.Println(signed)
	return nil
}

func (c *S3Client) presignV4(method, bucket, key string, expireSec int) (string, error) {
	pc := s3.NewPresignClient(c.S3)
	expires := time.Duration(expireSec) * time.Second
	switch method {
	case "GET":
		req, err := pc.PresignGetObject(c.Ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		}, func(o *s3.PresignOptions) { o.Expires = expires })
		if err != nil {
			return "", err
		}
		return req.URL, nil
	case "PUT":
		req, err := pc.PresignPutObject(c.Ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		}, func(o *s3.PresignOptions) { o.Expires = expires })
		if err != nil {
			return "", err
		}
		return req.URL, nil
	case "DELETE":
		req, err := pc.PresignDeleteObject(c.Ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		}, func(o *s3.PresignOptions) { o.Expires = expires })
		if err != nil {
			return "", err
		}
		return req.URL, nil
	case "HEAD":
		req, err := pc.PresignHeadObject(c.Ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket), Key: aws.String(key),
		}, func(o *s3.PresignOptions) { o.Expires = expires })
		if err != nil {
			return "", err
		}
		return req.URL, nil
	}
	return "", fmt.Errorf("unsupported method %s", method)
}

// 生成v2版本的预签名的URL, 只是为了兼容一部分私有化的情况，需要支持v2版本的签名来延长文件获取的有效期
func urlSignV2(AccessKey, SecretKey, Bucket, HostBase, filepath string, urlExpireSeconds int) string {
	params := url.Values{}
	params.Set("AWSAccessKeyId", AccessKey)
	params.Set("Expires", fmt.Sprintf("%d", time.Now().Add(time.Second*time.Duration(urlExpireSeconds)).Unix()))

	encodedFilepath := strings.ReplaceAll(url.PathEscape(filepath), "%2F", "/")
	strToSign := fmt.Sprintf("%s\n\n\n%s\n%s", "GET", params.Get("Expires"), fmt.Sprintf("/%s/%s", Bucket, encodedFilepath))
	h := hmac.New(sha1.New, []byte(SecretKey))
	h.Write([]byte(strToSign))

	params.Set("Signature", base64.StdEncoding.EncodeToString(h.Sum(nil)))
	baseURL := fmt.Sprintf("%s/%s/%s", HostBase, Bucket, encodedFilepath)
	urlStr := fmt.Sprintf("%s?%s", baseURL, params.Encode())
	return urlStr
}
