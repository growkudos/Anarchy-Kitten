package main

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestAwsCredentials(test *testing.T) {
	credentials := AwsCredentials()

	assert.Equal(test, credentials, "test", "This should fail")
}

func TestValidateAwsCredentialsValid(test *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID_VALUE")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY_VALUE")
	os.Setenv("AWS_REGION", "AWS_REGION_VALUE")

	err := ValidateAwsCredentials()

	assert.Equal(test, err, nil)

	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_REGION")
}

func TestValidateAwsCredentialsAreMissing(test *testing.T) {
	err := ValidateAwsCredentials()

	assert.EqualError(test, err, "AWS credentials not set")
}
