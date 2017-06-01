package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/stretchr/testify/assert"
)

func TestAwsCredentials(test *testing.T) {
	credentials := AwsCredentials()

	assert.Equal(test, credentials, "test", "This should fail")
}

func TestGetAuotscalingGroupInstanceIDs(test *testing.T) {
	mockASGInstanceIds := []string{
		"instanceIdOne",
		"instanceIdTwo",
		"instanceIdThree",
		"instanceIdFour",
		"instanceIdFive",
	}

	resp := getMockDescribeAutoScalingGroupsOutput(mockASGInstanceIds)

	instanceIDs := getAuotscalingGroupInstanceIDs(resp)

	for index, instanceID := range instanceIDs {
		assert.Equal(test, instanceID, mockASGInstanceIds[index], nil)
	}
}

func TestValidateAwsCredentialsValid(test *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	os.Setenv("AWS_REGION", "AWS_REGION_VALUE")

	err := ValidateAwsCredentials()

	assert.Equal(test, err, nil)

	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_REGION")
}

func TestValidateAwsCredentialsAreMissing(test *testing.T) {
	err := ValidateAwsCredentials()

	assert.EqualError(test, err, "AWS credentials not set")
}

func getInstanceList(instanceIDs []string) []*autoscaling.Instance {
	instances := []*autoscaling.Instance{}

	for index := range instanceIDs {
		instance := autoscaling.Instance{
			InstanceId: &instanceIDs[index],
		}

		instances = append(instances, &instance)
	}

	return instances
}

func getMockDescribeAutoScalingGroupsOutput(instanceIds []string) autoscaling.DescribeAutoScalingGroupsOutput {
	return autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			{
				Instances: getInstanceList(instanceIds),
			},
		},
	}
}
