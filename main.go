package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

type pollASGActivities func(
	*autoscaling.DescribeScalingActivitiesInput,
	autoscalingiface.AutoScalingAPI,
	string,
) (bool, error)

func main() {
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)

	sess := session.Must(session.NewSession())
	svc := autoscaling.New(sess)

	url, content, timeout, poll := getFlags(flag.CommandLine, os.Args[1:])

	os.Exit(do(
		svc,
		url,
		content,
		(time.Duration(timeout))*time.Second,
		(time.Duration(poll))*time.Second))
}

func getFlags(fs *flag.FlagSet, args []string) (string, string, int, int) {
	urlPtr := fs.String("url", "http://www.growkudos.com", "The url to check")
	contentPtr := fs.String("content", "Maintenance", "The content to check for")
	timeoutPtr := fs.Int("timeout", 600, "The timeout for the content poll check in seconds")
	pollPtr := fs.Int("poll", 10, "The content poll interval in seconds")
	fs.Parse(args)

	return *urlPtr, *contentPtr, *timeoutPtr, *pollPtr
}

func do(
	svc autoscalingiface.AutoScalingAPI,
	urlToCheck string,
	contentToCheckFor string,
	timeout time.Duration,
	pollEvery time.Duration,
) int {
	exitCode := 0

	err := validateAwsCredentials()
	if err != nil {
		log.WithError(err).Fatal("AWS environment variables needed")
	}

	asgName := os.Getenv("ASG_NAME")

	instanceIDs := getInstancesInAutoScalingGroup(&asgName, svc)
	result := enterStandby(asgName, svc, instanceIDs, timeout, pollEvery)

	if result == true {
		exitCode += checkForContentAtURL(urlToCheck, contentToCheckFor)
	} else {
		log.Info("All instances in the autoscaling group did not enter standby")
		exitCode++
	}

	exitCode += exitStandby(asgName, svc, instanceIDs, 60*time.Second, 1*time.Second)
	// TODO keep trying until success?

	log.WithFields(log.Fields{
		"extCode": exitCode,
	}).Info("Finished")

	return exitCode
}

func checkForContentAtURL(rawurl string, content string) int {
	log.WithFields(log.Fields{
		"rawurl":  rawurl,
		"content": content,
	}).Debug("checkForContentAtURL")

	_, err := url.ParseRequestURI(rawurl)
	if err != nil {
		log.
			WithError(err).
			WithField("rawurl", rawurl).
			Error("Could not parse the URL")
		return 1
	}

	res, err := http.Get(rawurl)
	if err != nil {
		log.
			WithError(err).
			WithField("rawurl", rawurl).
			Error("Could not get the URL")
		return 1
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.
			WithError(err).
			WithField("res", res).
			Error("Could not read the response body")
		return 1
	}

	exists := strings.Contains(string(body), content)
	if !exists {
		log.WithFields(log.Fields{
			"body":    string(body),
			"content": content,
		}).Error("Did not find the expected content at the failover url")
		return 1
	}

	return 0
}

func enterStandby(
	asgName string,
	svc autoscalingiface.AutoScalingAPI,
	instanceIDs []*string,
	timeout time.Duration,
	pollEvery time.Duration,
) bool {
	enterStandbyInput := getEnterStandbyInput(instanceIDs, &asgName)
	enterStandbyOutput, err := svc.EnterStandby(enterStandbyInput)
	if err != nil {
		log.WithFields(log.Fields{
			"err":                err,
			"enterStandbyOutput": enterStandbyOutput,
		}).Error("Error entering instances into standby")
		// We'll let the logic carry on, which may mean that the wait for
		// standby will timeout but will continue to attempt to put everything
		// back into service.
		// TODO or call Recover()?
		return false
	}

	activityIDs := []*string{enterStandbyOutput.Activities[0].ActivityId}
	result := waitForInstancesToReachSuccessfulStatus(
		&asgName,
		activityIDs,
		svc,
		timeout,
		pollEvery)

	return result
}

func exitStandby(
	asgName string,
	svc autoscalingiface.AutoScalingAPI,
	instanceIDs []*string,
	timeout time.Duration,
	pollEvery time.Duration,
) int {
	exitStandbyArgs := autoscaling.ExitStandbyInput{
		AutoScalingGroupName: &asgName,
		InstanceIds:          instanceIDs,
	}

	exitStandbyOutput, err := svc.ExitStandby(&exitStandbyArgs)
	if err != nil {
		log.WithFields(log.Fields{
			"exitStandbyOutput": exitStandbyOutput,
			"err":               err,
		}).Error("Error calling ExitStandby")
		return 1
	}

	activityIDs := []*string{exitStandbyOutput.Activities[0].ActivityId}

	result := waitForInstancesToReachSuccessfulStatus(
		&asgName,
		activityIDs,
		svc,
		timeout,
		pollEvery)
	if result != true {
		// TODO do we retry? Carry on?
		log.Error("Instances failed to reach successful status")
		return 1
	}

	// TODO check instances have left standby?

	return 0 // TODO return 1 if an error
}

func waitForInstancesToReachSuccessfulStatus(
	asgName *string,
	activityIDs []*string,
	svc autoscalingiface.AutoScalingAPI,
	timeout time.Duration,
	pollEvery time.Duration,
) bool {

	describeScalingActivitiesQueryParams := &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds:          activityIDs,
		AutoScalingGroupName: asgName,
		MaxRecords:           aws.Int64(1),
	}

	return handleASGActivityPolling(
		describeScalingActivitiesQueryParams,
		checkActivitiesForStatus,
		svc,
		timeout, // TODO pass in on commandline
		pollEvery,
		"Successful")
}

func getInstancesInAutoScalingGroup(
	asgName *string,
	svc autoscalingiface.AutoScalingAPI) []*string {
	instanceIDQueryParams := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			asgName,
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := svc.DescribeAutoScalingGroups(instanceIDQueryParams)
	if err != nil {
		log.WithError(err).Fatal("Coud not get instances in asg, DescribeAutoScalingGroups failed")
	}

	return getAutoscalingGroupInstanceIDs(resp)
}

func handleASGActivityPolling(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	pollFunc pollASGActivities,
	svc autoscalingiface.AutoScalingAPI,
	timeout time.Duration,
	pollEvery time.Duration,
	statusCode string,
) bool {

	log.WithFields(log.Fields{
		"describeActivityConfig": describeActivityConfig,
	}).Debug("handleASGActivityPolling: ASG describe input")

	var pollIteration int64

	for {
		if pollIteration >= (int64(timeout) / int64(pollEvery)) {
			break
		}

		success, err := pollFunc(describeActivityConfig, svc, statusCode)
		if err != nil {
			log.WithError(err).Error("Error waiting for ASG update")
			break
		}

		if success {
			return true
		}

		time.Sleep(pollEvery)
		pollIteration++
	}

	return false
}

func checkActivitiesForStatus(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	svc autoscalingiface.AutoScalingAPI,
	statusCode string,
) (bool, error) {

	resp, err := svc.DescribeScalingActivities(describeActivityConfig)
	if err != nil {
		log.WithFields(log.Fields{
			"response": resp,
			"err":      err,
		}).Error("DescribeScalingActivities failed")
		return false, err
	}

	finished := true

	for _, activity := range resp.Activities {
		if *activity.StatusCode != statusCode {
			finished = false
		}
	}

	return finished, err
}

func getDescribeScalingActivitiesInput(
	activityIDs []*string,
	resourceName *string) *autoscaling.DescribeScalingActivitiesInput {
	return &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds:          activityIDs,
		AutoScalingGroupName: resourceName,
		MaxRecords:           aws.Int64(1),
	}
}

func getEnterStandbyInput(
	instanceIDs []*string,
	resourceName *string) *autoscaling.EnterStandbyInput {
	ret := &autoscaling.EnterStandbyInput{
		AutoScalingGroupName:           resourceName,
		ShouldDecrementDesiredCapacity: aws.Bool(true),
		InstanceIds:                    instanceIDs,
	}

	log.WithFields(log.Fields{
		"EnterStandbyInput": ret,
	}).Debug("Query parameters for stand by")

	return ret
}

func getAutoscalingGroupInstanceIDs(
	resp *autoscaling.DescribeAutoScalingGroupsOutput) []*string {
	instanceIDs := []*string{}

	for _, instance := range resp.AutoScalingGroups[0].Instances {
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}

	log.WithFields(log.Fields{
		"instanceIDs": *instanceIDs[0],
	}).Debug("Instances in auto scaling group")

	return instanceIDs
}

func validateAwsCredentials() error {
	log.Info("Checking Credentials...")
	if isEnvVarSetWithValue("AWS_ACCESS_KEY_ID") &&
		isEnvVarSetWithValue("AWS_SECRET_ACCESS_KEY") &&
		isEnvVarSetWithValue("AWS_REGION") &&
		isEnvVarSetWithValue("ASG_NAME") {
		return nil
	}

	return errors.New("AWS credentials not set")
}

func isEnvVarSetWithValue(key string) bool {
	val, ok := os.LookupEnv(key)
	log.WithFields(log.Fields{
		"key": key,
		"val": val,
		"ok":  ok,
	}).Debug("isEnvVarSetWithValue")
	return ok && val != ""
}
