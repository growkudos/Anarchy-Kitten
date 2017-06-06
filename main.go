package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	// "github.com/aws/aws-sdk-go/service/ec2"
	"os"
)

type PollASGActivities func(*autoscaling.DescribeScalingActivitiesInput) (bool, error)

func main() {
	fmt.Println("Checking Credentials...")

	if ValidateAwsCredentials() != nil {
		fmt.Println("Credentials are invalid!")
		return
	}

	sess := session.Must(session.NewSession())

	svc := autoscaling.New(sess)

	// getAuotscalingGroupInstanceIDs (resp *autoscaling.DescribeAutoScalingGroupsInput)

	instanceIdQueryParams := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String("review-www"), // Required
			// More values...
		},
		MaxRecords: aws.Int64(1),
		// NextToken:  aws.String("XmlString"),
	}

	resp, err := svc.DescribeAutoScalingGroups(instanceIdQueryParams)

	if err != nil {
		// TODO investigate https://golang.org/pkg/log/#Fatalf
		fmt.Println("ERROR!")
		fmt.Println(err.Error())
		return
	}

	instanceIDs := getAuotscalingGroupInstanceIDs(resp)

	fmt.Println("%v", *instanceIDs[0])

	resourceName := os.Getenv("ASG_NAME")

	enterStandByQueryParams := getEnterStandbyInput(instanceIDs, &resourceName)

	fmt.Printf("%v", enterStandByQueryParams)

	enterStandByQueryResp, err := svc.EnterStandby(enterStandByQueryParams)

	fmt.Println("%v", enterStandByQueryResp)

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
			// TODO investigate https://golang.org/pkg/log/#Fatalf
			fmt.Println("ERROR!")
			fmt.Println(err.Error())
			break
			return
		}

		fmt.Println("%v", describeScalingActivitiesResp)

		time.Sleep(time.Second * 5)
	}

	fmt.Println("Everything is gravy!")
}

func AwsCredentials() string {
	return "test"
}

func handleASGActivityPolling(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
	pollFunc PollASGActivities,
	timeoutInDuration int,
	pollEvery int,
	duration time.Duration,
) bool {

	pollIteration := 0

	for {
		if pollIteration >= (timeoutInDuration / pollEvery) {
			break
		}

		success, err := pollFunc(describeActivityConfig)

		if err != nil {
			fmt.Println("ERROR waiting for ASG update!")
			fmt.Println(err.Error())
			break
		}

		if success {
			return true
		}

		time.Sleep(duration * time.Duration(pollEvery))

		pollIteration += 1
	}

	return false
}

func pollASGActivitiesForSuccess(
	describeActivityConfig *autoscaling.DescribeScalingActivitiesInput,
) (bool, error) {

	// TODO ABSOLUTLEY DISCUSTING!!!!!
	sess := session.Must(session.NewSession())
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

func ValidateAwsCredentials() error {
	AwsAccessKeyId := os.Getenv("AWS_ACCESS_KEY_ID") != ""
	AwsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
	AwsRegion := os.Getenv("AWS_REGION") != ""
	AsgName := os.Getenv("ASG_NAME") != ""

	if AwsAccessKeyId && AwsSecretAccessKey && AwsRegion && AsgName {
		return nil
	}

	return errors.New("AWS credentials not set")
}
