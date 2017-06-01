package main

import (
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	// "github.com/aws/aws-sdk-go/service/ec2"
	"os"
)

func main() {
	fmt.Println("Checking Credentials...")

	if ValidateAwsCredentials() != nil {
		fmt.Println("Credentials are invalid!")
		return
	}

	sess := session.Must(session.NewSession())

	svc := autoscaling.New(sess)

	// getAuotscalingGroupInstanceIDs (resp *autoscaling.DescribeAutoScalingGroupsInput)

	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			aws.String("review-www"), // Required
			// More values...
		},
		MaxRecords: aws.Int64(1),
		// NextToken:  aws.String("XmlString"),
	}

	resp, err := svc.DescribeAutoScalingGroups(params)

	if err != nil {
		// TODO investigate https://golang.org/pkg/log/#Fatalf
		fmt.Println("ERROR!")
		fmt.Println(err.Error())
		return
	}

	fmt.Printf("%v", resp)
	// fmt.Printf("%v", resp.AutoScalingGroups[0].Instances[0])

	fmt.Println("Everything is gravy!")
}

func AwsCredentials() string {
	return "test"
}

func getAuotscalingGroupInstanceIDs(resp autoscaling.DescribeAutoScalingGroupsOutput) []string {
	instanceIDs := []string{}

	for _, instance := range resp.AutoScalingGroups[0].Instances {
		instanceIDs = append(instanceIDs, *instance.InstanceId)
	}

	return instanceIDs
}

func ValidateAwsCredentials() error {
	AwsAccessKeyId := os.Getenv("AWS_ACCESS_KEY_ID") != ""
	AwsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
	AwsRegion := os.Getenv("AWS_REGION") != ""

	if AwsAccessKeyId && AwsSecretAccessKey && AwsRegion {
		return nil
	}

	return errors.New("AWS credentials not set")
}
