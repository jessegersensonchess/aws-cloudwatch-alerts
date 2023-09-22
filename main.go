// Generates CloudWatch alerts for common AWS infrastructure components.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/sts"
)

// AlarmDetails struct holds the necessary details for creating a CloudWatch alarm.
type AlarmDetails struct {
	Namespace          string
	MetricName         string
	ResourceID         string
	DimensionValues    []string
	DimensionNames     []string
	ComparisonOperator string
	EvaluationPeriods  int64
	Period             int64
	Statistic          string
	Unit               string
}

// getAccountId fetches the AWS account ID using STS.
func getAccountId(sess *session.Session) string {
	// Create a new client for the STS service.
	stsClient := sts.New(sess)

	// Get the caller identity (includes the account ID)
	callerIdentity, err := stsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		log.Fatalf("Failed to get caller identity: %v", err)
		os.Exit(1)
	}

	// Return the account ID
	return *callerIdentity.Account
}

// createAlarm creates a CloudWatch alarm based on provided details.
func createAlarm(cloudwatchClient *cloudwatch.CloudWatch, details AlarmDetails, alarmThreshold float64, snsTopicArn string) error {
	alarmName := fmt.Sprintf("%s_%s", details.MetricName, details.ResourceID)

	exists, err := alarmExists(cloudwatchClient, alarmName)
	if err != nil {
		return err
	}

	if !exists {
		dimensions := make([]*cloudwatch.Dimension, len(details.DimensionNames))
		for i, name := range details.DimensionNames {
			dimensions[i] = &cloudwatch.Dimension{
				Name:  aws.String(name),
				Value: aws.String(details.DimensionValues[i]),
			}
		}
		_, err := cloudwatchClient.PutMetricAlarm(&cloudwatch.PutMetricAlarmInput{
			AlarmName:          aws.String(alarmName),
			ComparisonOperator: aws.String(details.ComparisonOperator),
			EvaluationPeriods:  aws.Int64(details.EvaluationPeriods),
			MetricName:         aws.String(details.MetricName),
			Namespace:          aws.String(details.Namespace),
			Period:             aws.Int64(details.Period),
			Statistic:          aws.String(details.Statistic),
			Threshold:          aws.Float64(alarmThreshold),
			ActionsEnabled:     aws.Bool(true),
			AlarmActions:       []*string{aws.String(snsTopicArn)},
			AlarmDescription:   aws.String(fmt.Sprintf("Alarm when %s %s %f %s", details.MetricName, details.ComparisonOperator, alarmThreshold, details.Unit)),
			Dimensions:         dimensions,
			Unit:               aws.String(details.Unit),
		})
		if err != nil {
			return err
		}
		log.Printf("Alarm created for resource: %s with metric: %s", details.ResourceID, details.MetricName)
	}
	return nil
}

// alarmExists checks if a CloudWatch alarm with the given name already exists.
func alarmExists(cloudwatchClient *cloudwatch.CloudWatch, alarmName string) (bool, error) {
	alarms, err := cloudwatchClient.DescribeAlarms(&cloudwatch.DescribeAlarmsInput{
		AlarmNames: []*string{aws.String(alarmName)},
	})
	if err != nil {
		return false, err
	}

	return len(alarms.MetricAlarms) > 0, nil
}

// getRunningEC2Instances retrieves a list of currently running EC2 instances.
func getRunningEC2Instances(sess *session.Session) ([]*ec2.Instance, error) {
	ec2Client := ec2.New(sess)
	resp, err := ec2Client.DescribeInstances(&ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, err
	}

	var runningInstances []*ec2.Instance
	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			if *instance.State.Name == "running" {
				runningInstances = append(runningInstances, instance)
			}
		}
	}
	return runningInstances, nil
}

// createCloudwatchAlarmForEC2Instances creates CloudWatch alarms for running EC2 instances.
func createCloudwatchAlarmForEC2Instances(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	cloudwatchClient := cloudwatch.New(sess)

	// Retrieve running EC2 instances
	instances, err := getRunningEC2Instances(sess)
	if err != nil {
		log.Fatalf("Failed to get running EC2 instances: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, instance := range instances {
		wg.Add(1)
		go func(instance *ec2.Instance) {
			defer wg.Done()

			details := AlarmDetails{
				Namespace:          "AWS/EC2",
				MetricName:         "CPUUtilization",
				ComparisonOperator: "GreaterThanThreshold",
				ResourceID:         *instance.InstanceId,
				DimensionNames:     []string{"InstanceId"},
				DimensionValues:    []string{*instance.InstanceId},
				EvaluationPeriods:  2,
				Period:             900,
				Statistic:          "Average",
				Unit:               "Percent",
			}

			err := createAlarm(cloudwatchClient, details, alarmThreshold, snsTopicArn)
			if err != nil {
				log.Printf("Failed to create alarm for instance %s: %v", *instance.InstanceId, err)
			}
		}(instance)
	}

	wg.Wait()
	log.Println("Finished creating alarms for all running EC2 instances.")
}

// createCloudwatchAlarmForECSServices creates CloudWatch alarms for ECS services.
func createCloudwatchAlarmForECSServices(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	ecsClient := ecs.New(sess)
	cloudwatchClient := cloudwatch.New(sess)

	// List all clusters
	clusters, err := ecsClient.ListClusters(&ecs.ListClustersInput{})
	if err != nil {
		log.Fatalf("Failed to list clusters: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, clusterArn := range clusters.ClusterArns {
		if len(strings.Split(aws.StringValue(clusterArn), "/")) < 2 {
			log.Printf("Unexpected format for clusterArn: %v", aws.StringValue(clusterArn))
			return
		}
		clusterName := strings.Split(aws.StringValue(clusterArn), "/")[1]

		// List all services within the current cluster
		services, err := ecsClient.ListServices(&ecs.ListServicesInput{
			Cluster: aws.String(clusterName),
		})
		if err != nil {
			log.Printf("Failed to list services for cluster %s: %v", clusterName, err)
			continue
		}

		for _, serviceArn := range services.ServiceArns {
			if len(strings.Split(aws.StringValue(serviceArn), "/")) < 3 {
				log.Printf("Unexpected format for serviceArn: %v", aws.StringValue(serviceArn))
				return
			}
			serviceName := strings.Split(aws.StringValue(serviceArn), "/")[2]

			wg.Add(1)
			go func(clusterName, serviceName string) {
				defer wg.Done()

				// Details for CPUUtilization alarm
				cpuDetails := AlarmDetails{
					Namespace:          "AWS/ECS",
					MetricName:         "CPUUtilization",
					ComparisonOperator: "GreaterThanThreshold",
					ResourceID:         serviceName,
					DimensionNames:     []string{"ServiceName", "ClusterName"},
					DimensionValues:    []string{serviceName, clusterName},
					EvaluationPeriods:  2,
					Period:             900,
					Statistic:          "Average",
					Unit:               "Percent",
				}

				err := createAlarm(cloudwatchClient, cpuDetails, alarmThreshold, snsTopicArn)
				if err != nil {
					log.Printf("Failed to create CPUUtilization alarm for service %s in cluster %s: %v", serviceName, clusterName, err)
				}

				// Details for MemoryUtilization alarm
				memDetails := cpuDetails
				memDetails.MetricName = "MemoryUtilization"

				err = createAlarm(cloudwatchClient, memDetails, alarmThreshold, snsTopicArn)
				if err != nil {
					log.Printf("Failed to create MemoryUtilization alarm for service %s in cluster %s: %v", serviceName, clusterName, err)
				}

			}(clusterName, serviceName)
		}
	}

	wg.Wait()
	log.Println("Finished creating alarms for all ECS services.")
}

// createCloudwatchAlarmForRDSInstances creates CloudWatch alarms for RDS instances.
func createCloudwatchAlarmForRDSInstances(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	rdsClient := rds.New(sess)
	cloudwatchClient := cloudwatch.New(sess)

	// List all RDS instances
	resp, err := rdsClient.DescribeDBInstances(&rds.DescribeDBInstancesInput{})
	if err != nil {
		log.Fatalf("Failed to describe RDS instances: %v", err)
	}

	var wg sync.WaitGroup

	for _, instance := range resp.DBInstances {
		dbInstanceIdentifier := aws.StringValue(instance.DBInstanceIdentifier)
		size, _ := getRDSTotalStorageSpace(rdsClient, dbInstanceIdentifier)
		threshold := (float64(size) * ((100 - alarmThreshold) / 100))
		wg.Add(1)
		go func(dbInstanceIdentifier string) {
			defer wg.Done()

			details := AlarmDetails{
				Namespace:          "AWS/RDS",
				MetricName:         "FreeStorageSpace",
				ResourceID:         dbInstanceIdentifier,
				DimensionNames:     []string{"DBInstanceIdentifier"},
				DimensionValues:    []string{dbInstanceIdentifier},
				ComparisonOperator: "LessThanThreshold",
				EvaluationPeriods:  2,
				Period:             120,
				Statistic:          "Average",
				Unit:               "Bytes",
			}

			//err := createAlarm(cloudwatchClient, details, alarmThreshold, snsTopicArn)
			err := createAlarm(cloudwatchClient, details, threshold, snsTopicArn)
			if err != nil {
				log.Printf("Failed to create alarm for RDS instance %s: %v", dbInstanceIdentifier, err)
			}

		}(dbInstanceIdentifier)
	}

	wg.Wait()
}

// getRDSTotalStorageSpace retrieves the total storage space allocated for a given RDS instance.
func getRDSTotalStorageSpace(rdsClient *rds.RDS, dbInstanceIdentifier string) (int64, error) {
	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(dbInstanceIdentifier),
	}
	resp, err := rdsClient.DescribeDBInstances(input)
	if err != nil {
		return 0, err
	}

	if len(resp.DBInstances) > 0 && resp.DBInstances[0].AllocatedStorage != nil {
		totalStorageSpaceGB := *resp.DBInstances[0].AllocatedStorage
		totalStorageSpaceBytes := totalStorageSpaceGB * 1000000000
		return totalStorageSpaceBytes, nil
	}

	return 0, fmt.Errorf("no allocated storage information found for DBInstanceIdentifier '%s'", dbInstanceIdentifier)
}

// createCloudwatchAlarmForALBs creates CloudWatch alarms for Application Load Balancers.
func createCloudwatchAlarmForALBs(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	elbv2Client := elbv2.New(sess)
	cloudwatchClient := cloudwatch.New(sess)

	// List all ALBs
	resp, err := elbv2Client.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		log.Fatalf("Failed to describe Application Load Balancers: %v", err)
	}

	var wg sync.WaitGroup

	for _, lb := range resp.LoadBalancers {
		loadBalancerName := aws.StringValue(lb.LoadBalancerName)

		wg.Add(1)
		go func(loadBalancerName string) {
			defer wg.Done()

			details := AlarmDetails{
				Namespace:          "AWS/ApplicationELB",
				MetricName:         "HTTPCode_ELB_5xx_Count",
				ResourceID:         loadBalancerName,
				DimensionNames:     []string{"LoadBalancer"},
				DimensionValues:    []string{loadBalancerName},
				ComparisonOperator: "GreaterThanThreshold",
				EvaluationPeriods:  1,
				Period:             900,
				Statistic:          "Sum",
				Unit:               "Count",
			}

			err := createAlarm(cloudwatchClient, details, alarmThreshold, snsTopicArn)
			if err != nil {
				log.Printf("Failed to create alarm for Application Load Balancer %s: %v", loadBalancerName, err)
			}

		}(loadBalancerName)
	}

	wg.Wait()
}

func main() {
	// Command line flags for specifying AWS region and profile.
	region := flag.String("region", "us-east-2", "AWS region")
	profile := flag.String("profile", "4511dev", "AWS CLI profile name")
	flag.Parse()

	profileName := *profile
	regionName := *region
	alarmThreshold := 80.0
	if regionName == "" {
		os.Exit(1)
	}
	fmt.Println(regionName, profileName)

	// Create a new AWS session.
	sess, err := session.NewSessionWithOptions(session.Options{
		// remove Profile to run as Lambda function
		Profile: profileName,
		Config:  aws.Config{Region: aws.String(regionName)},
	})

	if err != nil {
		fmt.Println("failed to create session,", err)
		return
	}

	// Construct the SNS topic ARN.
	snsTopicArn := fmt.Sprintf("arn:aws:sns:%s:%s:jesse-test", regionName, getAccountId(sess))

	// Create CloudWatch alarms for various AWS resources.
	createCloudwatchAlarmForEC2Instances(sess, alarmThreshold, snsTopicArn)
	createCloudwatchAlarmForECSServices(sess, alarmThreshold, snsTopicArn)
	createCloudwatchAlarmForRDSInstances(sess, alarmThreshold, snsTopicArn)
	createCloudwatchAlarmForALBs(sess, 10, snsTopicArn)
}
