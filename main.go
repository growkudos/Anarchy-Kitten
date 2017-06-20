package main

import (
	"crypto/tls"
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

type contentCheck struct {
	url      string
	content  string
	user     string
	password string
	insecure bool
}

type pollASGActivities func(
	*autoscaling.DescribeScalingActivitiesInput,
	autoscalingiface.AutoScalingAPI,
	string,
) (bool, error)

func main() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	sess := session.Must(session.NewSession())
	svc := autoscaling.New(sess)

	url, content, timeout, poll, user, password, insecure := getFlags(flag.CommandLine, os.Args[1:])

	os.Exit(do(
		svc,
		contentCheck{url: url, content: content, user: user, password: password, insecure: insecure},
		(time.Duration(poll))*time.Second,
		(time.Duration(timeout))*time.Second))
}

func getFlags(fs *flag.FlagSet, args []string) (string, string, int, int, string, string, bool) {
	urlPtr := fs.String("url", "http://www.growkudos.com", "The url to check")
	contentPtr := fs.String("content", "Maintenance", "The content to check for")
	timeoutPtr := fs.Int("timeout", 600, "The timeout for the content poll check in seconds")
	pollPtr := fs.Int("poll", 10, "The content poll interval in seconds")
	userPtr := fs.String("user", "", "A user for basic authentication")
	pwdPtr := fs.String("pwd", "", "The password for the basic auth user")
	insecurePtr := fs.Bool("insecure", false, "Whether to ignore certificate TLS errors")
	fs.Parse(args)

	return *urlPtr, *contentPtr, *timeoutPtr, *pollPtr, *userPtr, *pwdPtr, *insecurePtr
}

func do(
	svc autoscalingiface.AutoScalingAPI,
	check contentCheck,
	poll time.Duration,
	timeout time.Duration,
) int {
	exitCode := 0

	asgName := os.Getenv("ASG_NAME")
	err := validateAwsCredentials()
	if err != nil {
		log.WithError(err).Fatal("AWS environment variables needed")
	}

	instanceIDs := getInstanceIDs(getInstancesInAutoScalingGroup(&asgName, svc))
	result := enterStandby(asgName, svc, instanceIDs, poll, timeout)
	exitCode += result

	if result == 0 {
		exitCode += pollForContent(check, poll, timeout, checkForContentAtURL)
	}

	// This tries forever to get all the instances back into service
	for !areAllInstancesInService(
		getInstancesInAutoScalingGroup(&asgName, svc)) {

		exitCode += exitStandby(
			asgName,
			svc,
			instanceIDs,
			poll,
			timeout,
			func(in bool) bool { return in },
		)
	}

	log.WithFields(log.Fields{
		"extCode": exitCode,
	}).Info("Finished")

	return exitCode
}

func areAllInstancesInService(instances []*autoscaling.Instance) bool {
	log.WithField("instances", instances).Debug("areAllInstancesInService")
	for _, i := range instances {
		if *(i.LifecycleState) != "InService" {
			log.WithField("instances", instances).Info("Some instances not in service")
			return false
		}
	}

	log.Info("All instances now in service")
	return true
}

func pollForContent(
	content contentCheck,
	poll time.Duration,
	timeout time.Duration,
	check func(contentCheck) int,
) int {

	done := make(chan bool)
	ticker := time.NewTicker(poll)
	go func() {
		for t := range ticker.C {
			log.WithField("t", t).Debug("Poll for content check")
			if check(content) == 0 {
				done <- true
			}
		}
	}()

	select {
	case _ = <-done:
		ticker.Stop()
		log.Info("Content check polling finished")
		return 0
	case <-time.After(timeout):
		ticker.Stop()
		log.Warn("Content check polling timed out")
		return 1
	}
}

func checkForContentAtURL(c contentCheck) int {
	log.WithFields(log.Fields{
		"rawurl":  c.url,
		"content": c.content,
	}).Debug("checkForContentAtURL")

	_, err := url.ParseRequestURI(c.url)
	if err != nil {
		log.
			WithError(err).
			WithField("rawurl", c.url).
			Error("Could not parse the URL")
		return 1
	}

	res, err := getURL(c.url, c.user, c.password, c.insecure)
	if err != nil {
		log.
			WithError(err).
			WithField("rawurl", c.url).
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

	exists := strings.Contains(string(body), c.content)
	if !exists {
		log.WithFields(log.Fields{
			"res":     res,
			"body":    string(body),
			"content": c.content,
		}).Error("Did not find the expected content at the failover url")
		return 1
	}

	return 0
}

func getURL(
	url string,
	user string,
	password string,
	insecure bool) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.WithError(err).Error("Creating request")
		return nil, err
	}

	if user != "" {
		req.SetBasicAuth(user, password)
	}

	client := &http.Client{}

	if insecure {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.Transport = tr
	}

	response, err := client.Do(req)
	if err != nil {
		log.WithError(err).Error("Request")
	}

	return response, err
}

func enterStandby(
	asgName string,
	svc autoscalingiface.AutoScalingAPI,
	instanceIDs []*string,
	poll time.Duration,
	timeout time.Duration,
) int {
	log.WithField("instanceIDs", instanceIDs).Info("Attempting to enter standby")

	ret := 0
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
		ret++
	}

	activityIDs := []*string{enterStandbyOutput.Activities[0].ActivityId}
	result := waitForInstancesToReachSuccessfulStatus(
		&asgName,
		activityIDs,
		svc,
		poll,
		timeout)

	if result == false {
		log.
			WithField("InstanceIDs", instanceIDs).
			Info("Some (or all) of the instances in the autoscaling group did not enter standby")
		ret++
	} else {
		log.
			WithField("instanceIDs", instanceIDs).
			Info("Instances now in standby")
	}

	return ret
}

func exitStandby(
	asgName string,
	svc autoscalingiface.AutoScalingAPI,
	instanceIDs []*string,
	poll time.Duration,
	timeout time.Duration,
	isSuccess func(bool) bool,
) int {
	log.WithField("instanceIDs", instanceIDs).Info("Attempting to exit standby")
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

	ret := 0
	retryAttempts := 3
	for i := 0; i < retryAttempts; i++ {
		result := waitForInstancesToReachSuccessfulStatus(
			&asgName,
			activityIDs,
			svc,
			poll,
			timeout)

		if isSuccess(result) {
			log.WithField("instanceIDs", instanceIDs).Info("Instances exited standby")
			ret = 0
			break
		}

		log.Error("Instances failed to reach successful status")
		ret++
	}

	return ret
}

func waitForInstancesToReachSuccessfulStatus(
	asgName *string,
	activityIDs []*string,
	svc autoscalingiface.AutoScalingAPI,
	poll time.Duration,
	timeout time.Duration,
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
		poll,
		timeout,
		"Successful")
}

func getInstancesInAutoScalingGroup(
	asgName *string,
	svc autoscalingiface.AutoScalingAPI) []*autoscaling.Instance {
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

	return resp.AutoScalingGroups[0].Instances
}

func handleASGActivityPolling(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	pollFunc pollASGActivities,
	svc autoscalingiface.AutoScalingAPI,
	poll time.Duration,
	timeout time.Duration,
	statusCode string,
) bool {

	log.WithFields(log.Fields{
		"describeActivityConfig": describeActivityConfig,
	}).Debug("handleASGActivityPolling: ASG describe input")

	var pollIteration int64

	for {
		if pollIteration >= (int64(timeout) / int64(poll)) {
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

		time.Sleep(poll)
		pollIteration++
		log.WithField("poll", pollIteration).Info("Polling ASG status")
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

func getInstanceIDs(
	instances []*autoscaling.Instance) []*string {
	instanceIDs := []*string{}

	for _, instance := range instances {
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
		log.Info("Credentials OK")
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
