package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Testing stufff")
}

func AwsCredentials() string {
	return "test"
}

func ValidateAwsCredentials() (bool, error) {
	AwsAccessKeyId := os.Getenv("AWS_ACCESS_KEY_ID") != ""
	AwsSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY") != ""
	AwsRegion := os.Getenv("AWS_REGION") != ""

	if AwsAccessKeyId && AwsSecretAccessKey && AwsRegion {
		return true, nil
	}

	return false, errors.New("AWS credentials not set")
}
