package main

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/stretchr/testify/assert"
)

func TestAwsCredentials(test *testing.T) {
	credentials := awsCredentials()

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

	instanceIDs := getAuotscalingGroupInstanceIDs(&resp)

	for index, instanceID := range instanceIDs {
		assert.Equal(test, *instanceID, mockASGInstanceIds[index], nil)
	}
}

func TestGetEnterStandbyInput(test *testing.T) {
	mockASGInstanceIds := []*string{
		aws.String("instanceIdOne"),
		aws.String("instanceIdTwo"),
		aws.String("instanceIdThree"),
		aws.String("instanceIdFour"),
		aws.String("instanceIdFive"),
	}

	resourceName := "ResourceName"

	enterStandbyInput := getEnterStandbyInput(mockASGInstanceIds, &resourceName)

	assert.Equal(test, *enterStandbyInput.AutoScalingGroupName, "ResourceName", nil)
	assert.Equal(test, *enterStandbyInput.ShouldDecrementDesiredCapacity, true, nil)

	for index := range mockASGInstanceIds {
		assert.Equal(test, *enterStandbyInput.InstanceIds[index], *mockASGInstanceIds[index], nil)
	}
}

func TestGetDescribeScalingActivitiesInput(test *testing.T) {
	mockActivityIds := []*string{
		aws.String("ActivityIdOne"),
		aws.String("ActivityIdTwo"),
		aws.String("ActivityIdThree"),
		aws.String("ActivityIdFour"),
		aws.String("ActivityIdFive"),
	}

	resourceName := "ResourceName"

	describeScalingActivitiesInput := getDescribeScalingActivitiesInput(mockActivityIds, &resourceName)

	assert.Equal(test, *describeScalingActivitiesInput.AutoScalingGroupName, "ResourceName", nil)

	for index := range mockActivityIds {
		assert.Equal(test, *describeScalingActivitiesInput.ActivityIds[index], *mockActivityIds[index], nil)
	}
}

func TestHandleASGActivityPolling(test *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := &session.Session{}
	pollFunc := func(*autoscaling.DescribeScalingActivitiesInput, *session.Session) (bool, error) {
		assert.Equal(test, pollIteration < 5, true)

		pollIteration += 1

		return (pollIteration == 4), nil
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(test, success, true)
}

func TestHandleASGActivityPollingWhenTimesOut(test *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := &session.Session{}
	pollFunc := func(*autoscaling.DescribeScalingActivitiesInput, *session.Session) (bool, error) {
		return false, nil
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(test, success, false)
}

func TestHandleASGActivityPollingErrorHandling(test *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := &session.Session{}
	pollFunc := func(*autoscaling.DescribeScalingActivitiesInput, *session.Session) (bool, error) {
		pollIteration += 1

		return false, errors.New("Test Error")
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(test, success, false)
	assert.Equal(test, pollIteration, 1)
}

func TestPollASGActivitiesForSuccess(test *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds: []*string{
			aws.String("ActivityIdOne"),
			aws.String("ActivityIdTwo"),
			aws.String("ActivityIdThree"),
		},
		AutoScalingGroupName: aws.String("ResourceName"),
		MaxRecords:           aws.Int64(1),
	}

	mockSess := unit.Session

	success, err := pollASGActivitiesForSuccess(mockConfig, mockSess)

	assert.Equal(test, success, true)
	assert.Equal(test, err, nil)
}

func TestValidateAwsCredentialsValid(test *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	os.Setenv("ASG_NAME", "ASG_NAME_VALUE")

	err := validateAwsCredentials()

	assert.Equal(test, err, nil)

	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_REGION")
	os.Unsetenv("ASG_NAME")
}

func TestValidateAwsCredentialsAreMissing(test *testing.T) {
	err := validateAwsCredentials()

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
