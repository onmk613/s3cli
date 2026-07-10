package action

import (
	"bytes"
	"encoding/json"
	"fmt"

	myprint "s3cli/pkg/fmtutil"
	"s3cli/pkg/utils"
)

func (c *S3Client) SetPolicy(policyfile, bucketname string) error {
	data, _, err := utils.LoadAWSConfigFile(policyfile)
	if err != nil {
		return err
	}
	if err := utils.ValidateJSON(data); err != nil {
		return err
	}
	if err := c.S3.SetBucketPolicy(c.Ctx, bucketname, data); err != nil {
		return fmt.Errorf("set policy %s: %s", bucketname, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Policy set for %s\n", c.S3Path(bucketname, ""))
	return nil
}

func (c *S3Client) GetPolicy(bucketname string) error {
	raw, err := c.S3.GetBucketPolicy(c.Ctx, bucketname)
	if err != nil {
		return fmt.Errorf("get policy %s: %s", bucketname, FormatAPIError(err))
	}
	myprint.PrintfBoldBlue("# %s policy:\n", c.S3Path(bucketname, ""))

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err == nil {
		myprint.PrintlnGreen(pretty.String())
	} else {
		myprint.PrintlnGreen(string(raw))
	}
	return nil
}

func (c *S3Client) DelPolicy(bucketname string) error {
	if err := c.S3.DeleteBucketPolicy(c.Ctx, bucketname); err != nil {
		return fmt.Errorf("delete policy %s: %s", bucketname, FormatAPIError(err))
	}

	myprint.PrintfBoldGreen("Policy deleted for %s: success\n", c.S3Path(bucketname, ""))
	return nil
}
