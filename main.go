package main

import (
	"errors"
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
	// "github.com/aws/aws-sdk-go/service/ec2"
)

type pollASGActivities func(
	*autoscaling.DescribeScalingActivitiesInput,
	autoscalingiface.AutoScalingAPI) (bool, error)

func main() {
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)

	exitCode := 1

	// TODO pass in
	urlToCheck := "http://www.growkudos.com"
	contentToCheckFor := "Maintenance"

	err := validateAwsCredentials()
	if err != nil {
		log.WithError(err).Fatal("AWS environment variables needed")
	}

	asgName := os.Getenv("ASG_NAME")
	sess := session.Must(session.NewSession())
	svc := autoscaling.New(sess)

	instanceIDs := getInstancesInAutoScalingGroup(&asgName, svc)
	enterStandbyInput := getEnterStandbyInput(instanceIDs, &asgName)
	enterStandbyOutput, err := svc.EnterStandby(enterStandbyInput)
	if err != nil {
		log.WithError(err).Error("Error entering instances into standby")
		// We'll let the logic carry on, which may mean that the wait for
		// standby will timeout but will continue to attempt to put everything
		// back into service.
	}

	result := waitForInstancesToEnterStandby(&asgName, enterStandbyOutput)
	if result == true {
		success, err2 := checkForContentAtURL(urlToCheck, contentToCheckFor)
		if err2 != nil || success == false {
			log.WithFields(log.Fields{
				"err":     err2,
				"success": success,
			}).Warn("Content not found at URL. Failover failed.")
		} else {
			exitCode = 0
		}
	} else {
		log.Info("All instances in the autoscaling group did not enter standby")
	}

	// TODO return the machines to in service
	exitStandbyArgs := autoscaling.ExitStandbyInput{
		AutoScalingGroupName: &asgName,
		InstanceIds:          instanceIDs,
	}

	exitStandbyOutput, err := svc.ExitStandby(&exitStandbyArgs)
	if err != nil {
		// TODO
	}

	log.Debug(exitStandbyOutput)

	// TODO Wait for instances to exit standby

	// TODO check instances leave standby

	log.Info("Success")

	os.Exit(exitCode)
}

func checkForContentAtURL(rawurl string, content string) (bool, error) {
	_, err := url.ParseRequestURI(rawurl)
	if err != nil {
		return false, err
	}

	res, err := http.Get(rawurl)
	if err != nil {
		return false, err
	}

	body, err := ioutil.ReadAll(res.Body)
	exists := strings.Contains(string(body), content)
	return exists, err
}

func waitForInstancesToEnterStandby(
	asgName *string,
	enterStandbyOutput *autoscaling.EnterStandbyOutput,
) bool {
	activityIDs := []*string{
		enterStandbyOutput.Activities[0].ActivityId,
	}

	describeScalingActivitiesQueryParams := &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds:          activityIDs,
		AutoScalingGroupName: asgName,
		MaxRecords:           aws.Int64(1),
	}

	return handleASGActivityPolling(
		describeScalingActivitiesQueryParams,
		checkActivitiesForSuccess,
		session.New(),
		42, // TODO pass in on commandline
		1,  // TODO
		time.Second)
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

	return getAuotscalingGroupInstanceIDs(resp)
}

/*
func pollCheck(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	checkFunc pollASGActivities,
	sess *session.Session,
	duration time.Duration,
	timeout time.Duration,
) bool {

	svc := autoscaling.New(sess)
	c := make(chan bool)
	ticker := time.NewTicker(duration)
	go func() {
		for range ticker.C {
			check, err := checkFunc(describeActivityConfig, svc)
			if check == true {
				c <- true
			}

			if err != nil {
				c <- false
			}
		}
	}()

	ret := false
	select {
	case result := <-c:
		// May have succeeded or failed
		ret = result
	case <-time.After(timeout):
		// timeout
	}

	ticker.Stop()
	return ret
}
*/

func handleASGActivityPolling(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	pollFunc pollASGActivities,
	sess *session.Session,
	timeoutInDuration int,
	pollEvery int,
	duration time.Duration,
) bool {

	log.WithFields(log.Fields{
		"describeActivityConfig": describeActivityConfig,
	}).Debug("handleASGActivityPolling: ASG describe input")

	pollIteration := 0
	svc := autoscaling.New(sess)

	for {
		if pollIteration >= (timeoutInDuration / pollEvery) {
			break
		}

		success, err := pollFunc(describeActivityConfig, svc)
		if err != nil {
			log.WithError(err).Error("Error waiting for ASG update")
			break
		}

		if success {
			return true
		}

		time.Sleep(duration * time.Duration(pollEvery))
		pollIteration++
	}

	return false
}

func checkActivitiesForSuccess(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	svc autoscalingiface.AutoScalingAPI,
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
		if *activity.StatusCode != "Successful" {
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

func getAuotscalingGroupInstanceIDs(
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
