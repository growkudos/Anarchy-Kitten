package main

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAwsCredentials(test *testing.T) {
	credentials := AwsCredentials()

	assert.Equal(test, credentials, "test", "This should fail")
}

// func TestValidateAwsCredentialsValid(test *testing.T) {}

func TestValidateAwsCredentialsAreMissing(test *testing.T) {
	valid, err := ValidateAwsCredentials()

	assert.Equal(test, valid, false)
	assert.EqualError(test, err, "AWS credentials not set")
}
