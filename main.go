package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Checking Credentials...")

	if ValidateAwsCredentials() != nil {
		return
	}

	fmt.Println("Everything is gravy!")
}

func AwsCredentials() string {
	return "test"
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
