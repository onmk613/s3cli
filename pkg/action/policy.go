package action

import (
	"bytes"
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// SetPolicy 设置 bucket policy (JSON 内容)
func (c *S3Client) SetPolicy(policyfile, bucketname string) error {
	data, _, err := utils.LoadAWSConfigFile(policyfile)
	if err != nil {
		return err
	}
	if err := utils.ValidateJSON(data); err != nil {
		return err
	}
	_, err = c.S3.PutBucketPolicy(c.Ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucketname), Policy: aws.String(string(data)),
	})
	if err != nil {
		return fmt.Errorf("set policy %s: %s", bucketname, FormatAPIError(err))
	}
	myprint.Printf("Policy set for %s\n", c.S3Path(bucketname, ""))
	return nil
}

// GetPolicy 打印 bucket policy
func (c *S3Client) GetPolicy(bucketname string) error {
	out, err := c.S3.GetBucketPolicy(c.Ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucketname)})
	if err != nil {
		return fmt.Errorf("get policy %s: %s", bucketname, FormatAPIError(err))
	}

	raw := aws.ToString(out.Policy)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(raw), "", "  "); err == nil {
		myprint.Printf("# %s\n%s\n", c.S3Path(bucketname, ""), pretty.String())
	} else {
		myprint.Printf("# %s\n%s\n", c.S3Path(bucketname, ""), raw)
	}
	return nil
}

// DelPolicy 删除 bucket policy
func (c *S3Client) DelPolicy(bucketname string) error {
	if _, err := c.S3.DeleteBucketPolicy(c.Ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucketname)}); err != nil {
		return fmt.Errorf("delete policy %s: %s", bucketname, FormatAPIError(err))
	}
	myprint.Printf("Policy deleted for %s\n", c.S3Path(bucketname, ""))
	return nil
}
