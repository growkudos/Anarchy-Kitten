package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	// "github.com/aws/aws-sdk-go/service/ec2"
)

type pollASGActivities func(*autoscaling.DescribeScalingActivitiesInput, *session.Session) (bool, error)

func main() {
	log.Info("Checking Credentials...")

	if validateAwsCredentials() != nil {
		log.Error("Credentials are invalid!")
		return
	}

	sess := session.Must(session.NewSession())

	svc := autoscaling.New(sess)

	// getAuotscalingGroupInstanceIDs (resp *autoscaling.DescribeAutoScalingGroupsInput)

	instanceIDQueryParams := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String("review-www"), // Required
			// More values...
		},
		MaxRecords: aws.Int64(1),
		// NextToken:  aws.String("XmlString"),
	}

	resp, err := svc.DescribeAutoScalingGroups(instanceIDQueryParams)

	if err != nil {
		log.WithError(err).Error("ERROR!")
		return
	}

	instanceIDs := getAuotscalingGroupInstanceIDs(resp)

	log.WithFields(log.Fields{
		"instanceIDs": *instanceIDs[0],
	}).Debug("Instances in auto scaling group")

	resourceName := os.Getenv("ASG_NAME")

	enterStandByQueryParams := getEnterStandbyInput(instanceIDs, &resourceName)

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
		AutoScalingGroupName: &resourceName,
		MaxRecords:           aws.Int64(1),
	}

	for i := 0; i <= 5; i++ {
		describeScalingActivitiesResp, err := svc.DescribeScalingActivities(describeScalingActivitiesQueryParams)

		if err != nil {
			log.WithError(err).Error("ERROR!")
			return
		}

		log.WithFields(log.Fields{
			"describeScalingActivitiesResp": describeScalingActivitiesResp,
		}).Debug("describe scaling activities response")

		time.Sleep(time.Second * 5)
	}

	log.Info("Success")
}

func awsCredentials() string {
	return "test"
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

	for {
		if pollIteration >= (timeoutInDuration / pollEvery) {
			break
		}

		success, err := pollFunc(describeActivityConfig, sess)

		if err != nil {
			fmt.Println("ERROR waiting for ASG update!")
			fmt.Println(err.Error())
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

func pollASGActivitiesForSuccess(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	sess *session.Session,
) (bool, error) {

	// TODO ABSOLUTLEY DISCUSTING!!!!!
	svc := autoscaling.New(sess)

	resp, err := svc.DescribeScalingActivities(describeActivityConfig)

	if err != nil {
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

func getDescribeScalingActivitiesInput(activityIDs []*string, resourceName *string) *autoscaling.DescribeScalingActivitiesInput {
	return &autoscaling.DescribeScalingActivitiesInput{
		ActivityIds:          activityIDs,
		AutoScalingGroupName: resourceName,
		MaxRecords:           aws.Int64(1),
	}
}

func getEnterStandbyInput(instanceIDs []*string, resourceName *string) *autoscaling.EnterStandbyInput {
	return &autoscaling.EnterStandbyInput{
		AutoScalingGroupName:           resourceName,
		ShouldDecrementDesiredCapacity: aws.Bool(true),
		InstanceIds:                    instanceIDs,
	}
}

func getAuotscalingGroupInstanceIDs(resp *autoscaling.DescribeAutoScalingGroupsOutput) []*string {
	instanceIDs := []*string{}

	for _, instance := range resp.AutoScalingGroups[0].Instances {
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}

	return instanceIDs
}

func validateAwsCredentials() error {
	AwsAccessKeyID := os.Getenv("AWS_ACCESS_KEY_ID") != ""
	AwsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
	AwsRegion := os.Getenv("AWS_REGION") != ""
	AsgName := os.Getenv("ASG_NAME") != ""

	if AwsAccessKeyID && AwsSecretAccessKey && AwsRegion && AsgName {
		return nil
	}

	return errors.New("AWS credentials not set")
}
