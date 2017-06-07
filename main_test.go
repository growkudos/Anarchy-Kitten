package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/stretchr/testify/assert"
)

// TODO set debug level to panic

type mockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	Error   bool
	Success bool
}

func (m *mockAutoScalingClient) DescribeScalingActivities(
	*autoscaling.DescribeScalingActivitiesInput) (
	*autoscaling.DescribeScalingActivitiesOutput, error) {
	statusCode := "Fail"
	if m.Success {
		statusCode = "Successful"
	}

	activity := autoscaling.Activity{StatusCode: aws.String(statusCode)}
	activities := []*autoscaling.Activity{&activity}
	resp := &autoscaling.DescribeScalingActivitiesOutput{Activities: activities}

	var err error
	if m.Error == true {
		err = errors.New("Error")
	}

	return resp, err
}

func TestGetAuotscalingGroupInstanceIDs(t *testing.T) {
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
		assert.Equal(t, *instanceID, mockASGInstanceIds[index], nil)
	}
}

func TestGetEnterStandbyInput(t *testing.T) {
	mockASGInstanceIds := []*string{
		aws.String("instanceIdOne"),
		aws.String("instanceIdTwo"),
		aws.String("instanceIdThree"),
		aws.String("instanceIdFour"),
		aws.String("instanceIdFive"),
	}

	resourceName := "ResourceName"

	enterStandbyInput := getEnterStandbyInput(mockASGInstanceIds, &resourceName)

	assert.Equal(t, *enterStandbyInput.AutoScalingGroupName, "ResourceName", nil)
	assert.Equal(t, *enterStandbyInput.ShouldDecrementDesiredCapacity, true, nil)

	for index := range mockASGInstanceIds {
		assert.Equal(
			t,
			*enterStandbyInput.InstanceIds[index],
			*mockASGInstanceIds[index],
			nil)
	}
}

func TestGetDescribeScalingActivitiesInput(t *testing.T) {
	mockActivityIds := []*string{
		aws.String("ActivityIdOne"),
		aws.String("ActivityIdTwo"),
		aws.String("ActivityIdThree"),
		aws.String("ActivityIdFour"),
		aws.String("ActivityIdFive"),
	}

	resourceName := "ResourceName"

	describeScalingActivitiesInput := getDescribeScalingActivitiesInput(
		mockActivityIds,
		&resourceName)

	assert.Equal(
		t,
		*describeScalingActivitiesInput.AutoScalingGroupName,
		"ResourceName",
		nil)

	for index := range mockActivityIds {
		assert.Equal(
			t,
			*describeScalingActivitiesInput.ActivityIds[index],
			*mockActivityIds[index],
			nil)
	}
}

/*
func TestPollCheck(t *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := unit.Session
	checkFunc :=
		func(
			*autoscaling.DescribeScalingActivitiesInput,
			autoscalingiface.AutoScalingAPI) (bool, error) {
			assert.Equal(t, pollIteration < 5, true)
			pollIteration++
			return (pollIteration == 4), nil
		}

	success := pollCheck(
		mockConfig,
		checkFunc,
		mockSess,
		time.Millisecond*1,
		time.Millisecond*5)

	assert.Equal(t, success, true)
}
*/

func TestHandleASGActivityPolling(t *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := unit.Session
	pollFunc :=
		func(
			*autoscaling.DescribeScalingActivitiesInput,
			autoscalingiface.AutoScalingAPI) (bool, error) {
			assert.Equal(t, pollIteration < 5, true)
			pollIteration++
			return (pollIteration == 4), nil
		}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(t, success, true)
}

func TestHandleASGActivityPollingWhenTimesOut(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := unit.Session
	pollFunc := func(
		*autoscaling.DescribeScalingActivitiesInput,
		autoscalingiface.AutoScalingAPI) (bool, error) {
		return false, nil
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(t, success, false)
}

func TestHandleASGActivityPollingErrorHandling(t *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSess := unit.Session
	pollFunc := func(
		*autoscaling.DescribeScalingActivitiesInput,
		autoscalingiface.AutoScalingAPI) (bool, error) {
		pollIteration++

		return false, errors.New("Test Error")
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSess, 5, 1, time.Millisecond)

	assert.Equal(t, success, false)
	assert.Equal(t, pollIteration, 1)
}

func TestPollASGActivitiesForSuccessError(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{Error: true}
	success, err := checkActivitiesForSuccess(mockConfig, mockSvc)

	assert.Equal(t, success, false)
	assert.EqualError(t, err, "Error")
}

func TestPollASGActivitiesForSuccessNotFinished(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{}
	success, err := checkActivitiesForSuccess(mockConfig, mockSvc)

	assert.Equal(t, false, success)
	assert.Equal(t, nil, err)
}

func TestPollASGActivitiesForSuccess(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}

	mockSvc := &mockAutoScalingClient{Success: true}
	success, err := checkActivitiesForSuccess(mockConfig, mockSvc)

	assert.Equal(t, success, true)
	assert.Equal(t, err, nil)
}

func TestValidateAwsCredentialsValid(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	err = os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	err = os.Setenv("ASG_NAME", "ASG_NAME_VALUE")
	assert.Nil(t, err)

	err = validateAwsCredentials()
	assert.Nil(t, err)

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	err = os.Unsetenv("AWS_REGION")
	err = os.Unsetenv("ASG_NAME")
	assert.Nil(t, err)
}

func TestValidateAwsCredentialsSomeAreMissing(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	assert.Nil(t, err)

	err = validateAwsCredentials()
	assert.EqualError(t, err, "AWS credentials not set")

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	assert.Nil(t, err)
}

func TestValidateAwsCredentialsAreMissing(t *testing.T) {
	err := validateAwsCredentials()

	assert.EqualError(t, err, "AWS credentials not set")
}

func TestCheckForContentAtURLInvalidUrl(t *testing.T) {
	_, err := checkForContentAtURL("Invalid", "test")
	assert.EqualError(t, err, "parse Invalid: invalid URI for request")
}

func TestCheckForContentAtURLIncorrectContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not matching")
	}))
	defer ts.Close()

	result, err := checkForContentAtURL(ts.URL, "test")
	assert.Nil(t, err)
	assert.False(t, result)
}

func TestCheckForContentAtURLCorrectContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	result, err := checkForContentAtURL(ts.URL, "matching")
	assert.Nil(t, err)
	assert.True(t, result)
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
