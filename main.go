package main

import (
	"crypto/tls"
	"errors"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type contentAuth struct {
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

	viper.AutomaticEnv()
	viper.SetDefault("poll", 10)
	viper.SetDefault("timeout", 600)
	viper.SetDefault("auth.insecure", false)
	viper.SetConfigName("config") // name of config file (without extension)
	viper.AddConfigPath(".")      // look for config in the working directory
	err := viper.ReadInConfig()   // Find and read the config file
	if err != nil {               // Handle errors reading the config file
		log.WithError(err).Panic("Fatal error trying to read the config file")
	}

	sess := session.Must(session.NewSession())
	svc := autoscaling.New(sess)

	os.Exit(do(
		svc,
		viper.GetString("primary"),
		viper.GetString("secondary"),
		viper.GetString("url"),
		contentAuth{
			user:     viper.GetString("auth.user"),
			password: viper.GetString("auth.password"),
			insecure: viper.GetBool("auth.insecure"),
		},
		(time.Duration(viper.GetInt("poll")))*time.Second,
		(time.Duration(viper.GetInt("timeout")))*time.Second))
}

func do(
	svc autoscalingiface.AutoScalingAPI,
	primary string,
	secondary string,
	u string,
	auth contentAuth,
	poll time.Duration,
	timeout time.Duration,
) int {
	log.WithFields(log.Fields{
		"primary":       primary,
		"secondary":     secondary,
		"poll":          poll,
		"timeout":       timeout,
		"auth.user":     auth.user,
		"auth.password": auth.password,
		"auth.insecure": auth.insecure,
	}).Info("Parameters")

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
		exitCode += pollForContent(secondary, u, auth, poll, timeout, checkForContentAtURL)
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

	// Now check that the content of the url is the original primary content
	exitCode += pollForContent(primary, u, auth, poll, timeout, checkForContentAtURL)

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
	content string,
	u string,
	auth contentAuth,
	poll time.Duration,
	timeout time.Duration,
	check func(string, string, contentAuth) int,
) int {

	done := make(chan bool)
	ticker := time.NewTicker(poll)
	go func() {
		for t := range ticker.C {
			log.WithField("t", t).Debug("Poll for content check")
			if check(content, u, auth) == 0 {
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

func checkForContentAtURL(content string, u string, auth contentAuth) int {
	log.WithFields(log.Fields{
		"url":     u,
		"content": content,
	}).Debug("checkForContentAtURL")

	_, err := neturl.ParseRequestURI(u)
	if err != nil {
		log.
			WithError(err).
			WithField("url", u).
			Error("Could not parse the URL")
		return 1
	}

	res, err := getURL(u, auth.user, auth.password, auth.insecure)
	if err != nil {
		log.
			WithError(err).
			WithField("url", u).
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
			"res":  res,
			"body": string(body),
		}).Debug("Did not find the expected content at the failover url")
		log.WithFields(log.Fields{
			"content": content,
		}).Warn("Did not find the expected content at the failover url")
		return 1
	}

	log.WithFields(log.Fields{
		"content": content,
		"url":     u,
	}).Info("Found the expected content")
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
	log.Info("Attempting to enter standby")

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
			Error("Some (or all) of the instances in the autoscaling group did not enter standby")
		ret++
	} else {
		log.
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
	log.Info("Attempting to exit standby")
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
			log.Info("Instances exited standby")
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
