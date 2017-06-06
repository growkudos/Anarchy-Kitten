package main

import (
	"errors"
	"os"
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

	log.Info("Checking Credentials...")

	if validateAwsCredentials() != nil {
		log.Error("Credentials are invalid!")
		return
	}

	asgName := os.Getenv("ASG_NAME")

	sess := session.Must(session.NewSession())
	svc := autoscaling.New(sess)

	instanceIDQueryParams := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String(asgName),
		},
		MaxRecords: aws.Int64(1),
	}

	resp, err := svc.DescribeAutoScalingGroups(instanceIDQueryParams)
	if err != nil {
		log.WithError(err).Error("DescribeAutoScalingGroups failed")
		return
	}

	instanceIDs := getAuotscalingGroupInstanceIDs(resp)

	log.WithFields(log.Fields{
		"instanceIDs": *instanceIDs[0],
	}).Debug("Instances in auto scaling group")

	enterStandByQueryParams := getEnterStandbyInput(instanceIDs, &asgName)

	log.WithFields(log.Fields{
		"enterStandByQueryParams": enterStandByQueryParams,
	}).Debug("Query parameters for stand by")

	enterStandByQueryResp, err := svc.EnterStandby(enterStandByQueryParams)

	log.WithFields(log.Fields{
		"enterStandByQueryResp": enterStandByQueryResp,
	}).Debug("Query response for stand by")

	activityIDs := []*string{
		enterStandByQueryResp.Activities[0].ActivityId,
	}

	describeScalingActivitiesQueryParams := &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds:          activityIDs,
		AutoScalingGroupName: &asgName,
		MaxRecords:           aws.Int64(1),
	}

	log.WithFields(log.Fields{
		"describeScalingActivitiesQueryParams": describeScalingActivitiesQueryParams,
	}).Debug("ASG describe input")

	// TODO poll
	handleASGActivityPolling(
		describeScalingActivitiesQueryParams,
		checkActivitiesForSuccess,
		session.New(),
		42, // TODO
		1,  // TODO
		time.Second)

	log.Info("Success")
}

func handleASGActivityPolling(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	pollFunc pollASGActivities,
	sess *session.Session,
	timeoutInDuration int,
	pollEvery int,
	duration time.Duration,
) bool {

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
	return &autoscaling.EnterStandbyInput{
		AutoScalingGroupName:           resourceName,
		ShouldDecrementDesiredCapacity: aws.Bool(true),
		InstanceIds:                    instanceIDs,
	}
}

func getAuotscalingGroupInstanceIDs(
	resp *autoscaling.DescribeAutoScalingGroupsOutput) []*string {
	instanceIDs := []*string{}

	for _, instance := range resp.AutoScalingGroups[0].Instances {
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}

	return instanceIDs
}

func validateAwsCredentials() error {
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
