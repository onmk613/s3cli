package action

import (
	"bytes"
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"
)

func (c *S3Client) SetPolicy(policyFile, bucket string) error {
	data, _, err := utils.LoadAWSConfigFile(policyFile)
	if err != nil {
		return err
	}
	if err := utils.ValidateJSON(data); err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucket, data); err != nil {
		return fmt.Errorf("set policy %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Policy set for %s\n", c.S3Path(bucket, ""))
	return nil
}

func (c *S3Client) GetPolicy(bucket string) error {
	raw, err := c.S3.GetBucketPolicy(c.Ctx, bucket)
	if err != nil {
		return fmt.Errorf("get policy %s: %s", bucket, FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s policy:\n", c.S3Path(bucket, ""))

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		myprint.PrintlnGreen(pretty.String())
	} else {
		myprint.PrintlnGreen(string(raw))
	}
	return nil
}

func (c *S3Client) DelPolicy(bucket string) error {
	if err := c.S3.DeleteBucketPolicy(c.Ctx, bucket); err != nil {
		return fmt.Errorf("delete policy %s: %s", bucket, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Policy deleted for %s: success\n", c.S3Path(bucket, ""))
	return nil
}
