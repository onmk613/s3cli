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

	myprint.PrintfBoldGreen("Policy set for %s\n", c.S3Path(bucketname, ""))
	return nil
}

func (c *S3Client) GetPolicy(bucketname string) error {
	out, err := c.S3.GetBucketPolicy(c.Ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucketname)})
	if err != nil {
		return fmt.Errorf("get policy %s: %s", bucketname, FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s policy:\n", c.S3Path(bucketname, ""))

	raw := aws.ToString(out.Policy)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(raw), "", "  "); err == nil {
		myprint.PrintlnGreen(pretty.String())
	} else {
		myprint.PrintlnGreen(raw)
	}
	return nil
}

func (c *S3Client) DelPolicy(bucketname string) error {
	if _, err := c.S3.DeleteBucketPolicy(c.Ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucketname)}); err != nil {
		return fmt.Errorf("delete policy %s: %s", bucketname, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Policy deleted for %s: success\n", c.S3Path(bucketname, ""))
	return nil
}
