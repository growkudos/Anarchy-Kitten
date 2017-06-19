package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	log.SetLevel(log.PanicLevel)
	os.Exit(m.Run())
}

type mockAutoScalingClient struct {
	autoscalingiface.AutoScalingAPI
	Error         string
	Success       bool
	ServiceStatus []string
	describeCount int
}

func (m *mockAutoScalingClient) DescribeAutoScalingGroups(
	*autoscaling.DescribeAutoScalingGroupsInput) (
	*autoscaling.DescribeAutoScalingGroupsOutput, error) {

	status := "InService"

	log.WithFields(log.Fields{
		"ServiceStatus": m.ServiceStatus,
		"describeCount": m.describeCount,
	}).Debug("mock describe")

	if len(m.ServiceStatus) > m.describeCount {
		status = m.ServiceStatus[m.describeCount]
	}

	output := autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			&autoscaling.Group{Instances: []*autoscaling.Instance{
				&autoscaling.Instance{
					InstanceId:     aws.String("instance1"),
					LifecycleState: aws.String(status)},
				&autoscaling.Instance{
					InstanceId:     aws.String("instance2"),
					LifecycleState: aws.String(status)},
				&autoscaling.Instance{
					InstanceId:     aws.String("instance3"),
					LifecycleState: aws.String(status)},
			}},
		},
	}

	m.describeCount++
	return &output, nil
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
	if m.Error == "DescribeScalingActivities" {
		err = errors.New("Error")
	}

	return resp, err
}

func (m *mockAutoScalingClient) EnterStandby(
	input *autoscaling.EnterStandbyInput) (*autoscaling.EnterStandbyOutput, error) {
	ret := autoscaling.EnterStandbyOutput{
		Activities: []*autoscaling.Activity{
			&autoscaling.Activity{ActivityId: aws.String("activity1")},
		},
	}

	var err error
	if m.Error == "EnterStandby" {
		err = errors.New("Error")
	}

	return &ret, err
}

func (m *mockAutoScalingClient) ExitStandby(
	*autoscaling.ExitStandbyInput) (*autoscaling.ExitStandbyOutput, error) {
	ret := autoscaling.ExitStandbyOutput{
		Activities: []*autoscaling.Activity{
			&autoscaling.Activity{ActivityId: aws.String("activity1")},
		},
	}

	var err error
	if m.Error == "ExitStandby" {
		err = errors.New("Error")
	}

	return &ret, err
}

func TestGetInstanceIDs(t *testing.T) {
	mockASGInstanceIds := []string{
		"instanceIdOne",
		"instanceIdTwo",
		"instanceIdThree",
		"instanceIdFour",
		"instanceIdFive",
	}

	resp := getMockDescribeAutoScalingGroupsOutput(mockASGInstanceIds)
	log.Debug(resp)

	instanceIDs := getInstanceIDs(resp.AutoScalingGroups[0].Instances)

	for index, instanceID := range instanceIDs {
		assert.Equal(t, *instanceID, mockASGInstanceIds[index], nil)
	}
}

func TestGetInstancesInAutoScalingGroup(t *testing.T) {
	mockASGInstanceIds := []string{
		"instance1",
		"instance2",
		"instance3",
	}

	mockSvc := &mockAutoScalingClient{Success: true}
	instances := getInstancesInAutoScalingGroup(aws.String("test"), mockSvc)

	for index, instance := range instances {
		assert.Equal(t, mockASGInstanceIds[index], *(*instance).InstanceId, nil)
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

func TestHandleASGActivityPolling(t *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{Success: true}
	pollFunc :=
		func(
			*autoscaling.DescribeScalingActivitiesInput,
			autoscalingiface.AutoScalingAPI,
			string) (bool, error) {
			assert.Equal(t, pollIteration < 5, true)
			pollIteration++
			return (pollIteration == 4), nil
		}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSvc, 5*time.Millisecond, 1*time.Millisecond, "Successful")

	assert.Equal(t, success, true)
}

func TestHandleASGActivityPollingWhenTimesOut(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{Success: true}
	pollFunc := func(
		*autoscaling.DescribeScalingActivitiesInput,
		autoscalingiface.AutoScalingAPI,
		string) (bool, error) {
		return false, nil
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSvc, 5*time.Millisecond, 1*time.Millisecond, "Successful")

	assert.Equal(t, success, false)
}

func TestHandleASGActivityPollingErrorHandling(t *testing.T) {
	pollIteration := 0
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{Success: true}
	pollFunc := func(
		*autoscaling.DescribeScalingActivitiesInput,
		autoscalingiface.AutoScalingAPI,
		string) (bool, error) {
		pollIteration++

		return false, errors.New("Test Error")
	}

	success := handleASGActivityPolling(mockConfig, pollFunc, mockSvc, 5*time.Millisecond, 1*time.Millisecond, "Successful")

	assert.Equal(t, success, false)
	assert.Equal(t, pollIteration, 1)
}

func TestPollASGActivitiesForSuccessError(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{Error: "DescribeScalingActivities"}
	success, err := checkActivitiesForStatus(mockConfig, mockSvc, "Successful")

	assert.Equal(t, success, false)
	assert.EqualError(t, err, "Error")
}

func TestPollASGActivitiesForSuccessNotFinished(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}
	mockSvc := &mockAutoScalingClient{}
	success, err := checkActivitiesForStatus(mockConfig, mockSvc, "Successful")

	assert.Equal(t, false, success)
	assert.Equal(t, nil, err)
}

func TestPollASGActivitiesForSuccess(t *testing.T) {
	mockConfig := &autoscaling.DescribeScalingActivitiesInput{}

	mockSvc := &mockAutoScalingClient{Success: true}
	success, err := checkActivitiesForStatus(mockConfig, mockSvc, "Successful")

	assert.Equal(t, success, true)
	assert.Equal(t, err, nil)
}

func TestWaitForInstancesToReachSuccessfulState(t *testing.T) {

	mockSvc := &mockAutoScalingClient{Success: true}
	success := waitForInstancesToReachSuccessfulStatus(
		aws.String("test"),
		[]*string{aws.String("test")},
		mockSvc,
		10*time.Millisecond,
		1*time.Millisecond)

	assert.Equal(t, success, true)
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
	assert.Equal(t, 1, checkForContentAtURL("Invalid", "test", "", "", false))
}

func TestCheckForContentAtURLIncorrectContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not matching")
	}))
	defer ts.Close()

	assert.Equal(t, 1, checkForContentAtURL(ts.URL, "test", "", "", false))
}

func TestCheckForContentAtURLCorrectContent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	assert.Equal(t, 0, checkForContentAtURL(ts.URL, "matching", "", "", false))
}

func TestGetURLSecureNoAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	resp, err := getURL(ts.URL, "", "", false)
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	body, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Equal(t, "matching\n", string(body))
}

func TestGetURLSecureAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "USER", user)
		assert.Equal(t, "PASSWORD", password)
	}))
	defer ts.Close()

	resp, err := getURL(ts.URL, "USER", "PASSWORD", false)
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetURLInsecureAuth(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "USER", user)
		assert.Equal(t, "PASSWORD", password)
	}))
	defer ts.Close()

	resp, err := getURL(ts.URL, "USER", "PASSWORD", true)
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetURLTLSInsecureNoAuth(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	resp, err := getURL(ts.URL, "", "", true)
	assert.Nil(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 200, resp.StatusCode)
	body, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Equal(t, "matching\n", string(body))
}

func TestExitStandbySuccess(t *testing.T) {
	isSuccess := func(in bool) bool { return in }
	mockSvc := &mockAutoScalingClient{Success: true}
	instances := []*string{aws.String("instance1")}
	assert.Equal(t, 0, exitStandby(
		"test",
		mockSvc,
		instances,
		9*time.Millisecond,
		1*time.Millisecond,
		isSuccess))
}

func TestExitStandbyExitCallFail(t *testing.T) {
	isSuccess := func(in bool) bool { return in }
	mockSvc := &mockAutoScalingClient{Error: "ExitStandby"}
	instances := []*string{aws.String("instance1")}
	assert.Equal(t, 1, exitStandby(
		"test",
		mockSvc,
		instances,
		9*time.Millisecond,
		1*time.Millisecond,
		isSuccess))
}

func TestExitStandbyWaitFail(t *testing.T) {
	isSuccess := func(in bool) bool { return in }
	mockSvc := &mockAutoScalingClient{Error: "DescribeScalingActivities"}
	instances := []*string{aws.String("instance1")}
	assert.Equal(t, 3, exitStandby(
		"test",
		mockSvc,
		instances,
		9*time.Millisecond,
		1*time.Millisecond,
		isSuccess))
}

func TestExitStandbySecondAttempt(t *testing.T) {
	loop := 0
	isSuccess := func(in bool) bool {
		loop++
		if loop == 2 {
			return true
		}
		return false
	}

	mockSvc := &mockAutoScalingClient{Error: "DescribeScalingActivities"}
	instances := []*string{aws.String("instance1")}
	assert.Equal(t, 0, exitStandby(
		"test",
		mockSvc,
		instances,
		9*time.Millisecond,
		1*time.Millisecond,
		isSuccess))
}

func TestDoSuccess(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	err = os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	err = os.Setenv("ASG_NAME", "ASG_NAME_VALUE")
	assert.Nil(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	mockSvc := &mockAutoScalingClient{Success: true}
	check := contentCheck{url: ts.URL, content: "matching"}
	exitCode := do(mockSvc, check, 3*time.Millisecond, 1*time.Millisecond)
	assert.Equal(t, 0, exitCode)

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	err = os.Unsetenv("AWS_REGION")
	err = os.Unsetenv("ASG_NAME")
	assert.Nil(t, err)
}

func TestDoEnterStandbyFail(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	err = os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	err = os.Setenv("ASG_NAME", "ASG_NAME_VALUE")
	assert.Nil(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	mockSvc := &mockAutoScalingClient{Error: "EnterStandby", Success: true}
	check := contentCheck{url: ts.URL, content: "matching"}
	exitCode := do(mockSvc, check, 3*time.Millisecond, 1*time.Millisecond)
	assert.Equal(t, 1, exitCode)

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	err = os.Unsetenv("AWS_REGION")
	err = os.Unsetenv("ASG_NAME")
	assert.Nil(t, err)
}

func TestDoContentCheckFail(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	err = os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	err = os.Setenv("ASG_NAME", "ASG_NAME_VALUE")
	assert.Nil(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	mockSvc := &mockAutoScalingClient{Success: true}
	check := contentCheck{url: ts.URL, content: "notMatching"}
	exitCode := do(mockSvc, check, 3*time.Millisecond, 1*time.Millisecond)
	assert.Equal(t, 1, exitCode)

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	err = os.Unsetenv("AWS_REGION")
	err = os.Unsetenv("ASG_NAME")
	assert.Nil(t, err)
}

func TestDoExitStandbyFail(t *testing.T) {
	err := os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	err = os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	err = os.Setenv("AWS_REGION", "AWS_REGION_VALUE")
	err = os.Setenv("ASG_NAME", "ASG_NAME_VALUE")
	assert.Nil(t, err)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "matching")
	}))
	defer ts.Close()

	mockSvc := &mockAutoScalingClient{
		Error:         "ExitStandby",
		Success:       true,
		ServiceStatus: []string{"Pending", "Pending", "InService"},
	}
	check := contentCheck{url: ts.URL, content: "matching"}
	exitCode := do(mockSvc, check, 3*time.Millisecond, 1*time.Millisecond)
	assert.Equal(t, 1, exitCode)

	err = os.Unsetenv("AWS_ACCESS_KEY_ID")
	err = os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	err = os.Unsetenv("AWS_REGION")
	err = os.Unsetenv("ASG_NAME")
	assert.Nil(t, err)
}

func TestGetFlagsDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	url, content, timeout, poll, user, password, insecure := getFlags(fs, nil)
	assert.Equal(t, "http://www.growkudos.com", url)
	assert.Equal(t, "Maintenance", content)
	assert.Equal(t, 600, timeout)
	assert.Equal(t, 10, poll)
	assert.Equal(t, "", user)
	assert.Equal(t, "", password)
	assert.Equal(t, false, insecure)
}

func TestGetFlagsSetValues(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	args := []string{
		"-url=TEST",
		"-content=CONTENT",
		"-timeout=42",
		"-poll=84",
		"-user=USER",
		"-pwd=PASSWORD",
		"-insecure=true"}
	url, content, timeout, poll, user, password, insecure := getFlags(fs, args)

	assert.Equal(t, "TEST", url)
	assert.Equal(t, "CONTENT", content)
	assert.Equal(t, 42, timeout)
	assert.Equal(t, 84, poll)
	assert.Equal(t, "USER", user)
	assert.Equal(t, "PASSWORD", password)
	assert.Equal(t, true, insecure)
}

func TestAreAllInstancesInServiceAllInService(t *testing.T) {
	instances := []*autoscaling.Instance{
		&autoscaling.Instance{
			LifecycleState: aws.String("InService")},
		&autoscaling.Instance{
			LifecycleState: aws.String("InService")},
		&autoscaling.Instance{
			LifecycleState: aws.String("InService")},
	}

	assert.True(t, areAllInstancesInService(instances))
}

func TestAreAllInstancesInServiceNoneInService(t *testing.T) {
	instances := []*autoscaling.Instance{
		&autoscaling.Instance{
			LifecycleState: aws.String("Pending")},
		&autoscaling.Instance{
			LifecycleState: aws.String("Pending")},
		&autoscaling.Instance{
			LifecycleState: aws.String("Pending")},
	}

	assert.False(t, areAllInstancesInService(instances))
}

func TestAreAllInstancesInServiceSomeInService(t *testing.T) {
	instances := []*autoscaling.Instance{
		&autoscaling.Instance{
			LifecycleState: aws.String("InService")},
		&autoscaling.Instance{
			LifecycleState: aws.String("Pending")},
		&autoscaling.Instance{
			LifecycleState: aws.String("Pending")},
	}

	assert.False(t, areAllInstancesInService(instances))
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
