// main_test.go

package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/golang/mock/gomock"
)

// Mock for the STS API
type mockSTSAPI struct {
	stsiface.STSAPI
	ctrl *gomock.Controller
}

func (m *mockSTSAPI) GetCallerIdentity(input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
	// Mocked response
	return &sts.GetCallerIdentityOutput{
		Account: aws.String("123456789012"),
	}, nil
}

// Mock for the CloudWatch API
type mockCloudWatchAPI struct {
	cloudwatchiface.CloudWatchAPI
	ctrl *gomock.Controller
}

func (m *mockCloudWatchAPI) DescribeAlarms(input *cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
	// Mocked response
	return &cloudwatch.DescribeAlarmsOutput{
		MetricAlarms: []*cloudwatch.MetricAlarm{
			{
				AlarmName: aws.String("TestAlarm"),
			},
		},
	}, nil
}

func TestGetAccountId(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSTS := &mockSTSAPI{ctrl: ctrl}
	accountID := getAccountId(mockSTS)
	if accountID != "123456789012" {
		t.Errorf("Expected account ID 123456789012, but got %s", accountID)
	}
}

func TestAlarmExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCW := &mockCloudWatchAPI{ctrl: ctrl}
	exists, err := alarmExists(mockCW, "TestAlarm")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !exists {
		t.Error("Expected alarm to exist")
	}
}
