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

func createDimensions(details AlarmDetails) []*cloudwatch.Dimension {
	dimensions := make([]*cloudwatch.Dimension, len(details.DimensionNames))
	for i, name := range details.DimensionNames {
		dimensions[i] = &cloudwatch.Dimension{
			Name:  aws.String(name),
			Value: aws.String(details.DimensionValues[i]),
		}
	}
	return dimensions
}

// createAlarm creates a CloudWatch alarm based on provided details.
func createAlarm(cloudwatchClient *cloudwatch.CloudWatch, details AlarmDetails, alarmThreshold float64, snsTopicArn string) error {
	alarmName := fmt.Sprintf("%s_%s", details.MetricName, details.ResourceID)

	exists, err := alarmExists(cloudwatchClient, alarmName)
	if err != nil {
		return err
	}

	dimensions := createDimensions(details)

	alarmInput := &cloudwatch.PutMetricAlarmInput{
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
		AlarmDescription:   aws.String(fmt.Sprintf("Alarm when %s %s %f", details.MetricName, details.ComparisonOperator, alarmThreshold)),
		Dimensions:         dimensions,
	}

	// Conditionally set the Unit field
	if details.Unit != "" {
		alarmInput.Unit = aws.String(details.Unit)
		alarmInput.AlarmDescription = aws.String(fmt.Sprintf("Alarm when %s %s %f %s", details.MetricName, details.ComparisonOperator, alarmThreshold, details.Unit))
	}

	_, err = cloudwatchClient.PutMetricAlarm(alarmInput)
	if err != nil {
		return err
	}

	if exists {
		log.Printf("Alarm updated for resource: %s with metric: %s", details.ResourceID, details.MetricName)
	} else {
		log.Printf("Alarm created for resource: %s with metric: %s", details.ResourceID, details.MetricName)
	}

	return nil
}

// alarmExists checks if the provided CloudWatch alarm name already exists.
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

// handleEC2Instance creates an alarm for a given EC2 instance.
func handleEC2Instance(cloudwatchClient *cloudwatch.CloudWatch, instance *ec2.Instance, alarmThreshold float64, snsTopicArn string) {
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
}

// createAlarmForInstances processes and creates alarms for all instances provided.
func createAlarmForInstances(cloudwatchClient *cloudwatch.CloudWatch, instances []*ec2.Instance, alarmThreshold float64, snsTopicArn string) {
	var wg sync.WaitGroup

	for _, instance := range instances {
		wg.Add(1)
		go func(instance *ec2.Instance) {
			defer wg.Done()
			handleEC2Instance(cloudwatchClient, instance, alarmThreshold, snsTopicArn)
		}(instance)
	}

	wg.Wait()
}

func createCloudwatchAlarmForEC2Instances(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	cloudwatchClient := cloudwatch.New(sess)

	// Retrieve running EC2 instances
	instances, err := getRunningEC2Instances(sess)
	if err != nil {
		log.Fatalf("Failed to get running EC2 instances: %v", err)
		return
	}

	createAlarmForInstances(cloudwatchClient, instances, alarmThreshold, snsTopicArn)
	log.Println("Finished creating alarms for all running EC2 instances.")
}

func extractClusterName(clusterArn string) (string, error) {
	parts := strings.Split(clusterArn, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected format for clusterArn: %v", clusterArn)
	}
	return parts[1], nil
}

func extractServiceName(serviceArn string) (string, error) {
	parts := strings.Split(serviceArn, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("unexpected format for serviceArn: %v", serviceArn)
	}
	return parts[2], nil
}

func handleService(cloudwatchClient *cloudwatch.CloudWatch, clusterName, serviceName string, alarmThreshold float64, snsTopicArn string) {
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
}

// Handle processing for a single cluster
func handleCluster(clusterArn *string, ecsClient *ecs.ECS, cloudwatchClient *cloudwatch.CloudWatch, alarmThreshold float64, snsTopicArn string, wg *sync.WaitGroup) {
	clusterName, err := extractClusterName(aws.StringValue(clusterArn))
	if err != nil {
		log.Println(err)
		return
	}

	// List all services within the current cluster
	services, err := ecsClient.ListServices(&ecs.ListServicesInput{
		Cluster: aws.String(clusterName),
	})
	if err != nil {
		log.Printf("Failed to list services for cluster %s: %v", clusterName, err)
		return
	}

	for _, serviceArn := range services.ServiceArns {
		serviceName, err := extractServiceName(aws.StringValue(serviceArn))
		if err != nil {
			log.Println(err)
			return
		}

		wg.Add(1)
		go func(clusterName, serviceName string) {
			defer wg.Done()
			handleService(cloudwatchClient, clusterName, serviceName, alarmThreshold, snsTopicArn)
		}(clusterName, serviceName)
	}
}

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
		handleCluster(clusterArn, ecsClient, cloudwatchClient, alarmThreshold, snsTopicArn, &wg)
	}

	wg.Wait()
	log.Println("Finished creating alarms for all ECS services.")
}

// listRDSInstances lists all RDS instances.
func listRDSInstances(client *rds.RDS) ([]*rds.DBInstance, error) {
	resp, err := client.DescribeDBInstances(&rds.DescribeDBInstancesInput{})
	if err != nil {
		return nil, err
	}
	return resp.DBInstances, nil
}

// handleRDSInstance creates an alarm for a given RDS instance.
func handleRDSInstance(cloudwatchClient *cloudwatch.CloudWatch, rdsClient *rds.RDS, dbInstanceIdentifier string, alarmThreshold float64, snsTopicArn string) {
	size, _ := getRDSTotalStorageSpace(rdsClient, dbInstanceIdentifier)
	threshold := (float64(size) * ((100 - alarmThreshold) / 100))

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

	err := createAlarm(cloudwatchClient, details, threshold, snsTopicArn)
	if err != nil {
		log.Printf("Failed to create alarm for RDS instance %s: %v", dbInstanceIdentifier, err)
	}
}

// createCloudwatchAlarmForRDSInstances creates CloudWatch alarms for RDS instances.
func createCloudwatchAlarmForRDSInstances(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	rdsClient := rds.New(sess)
	cloudwatchClient := cloudwatch.New(sess)

	dbInstances, err := listRDSInstances(rdsClient)
	if err != nil {
		log.Fatalf("Failed to list RDS instances: %v", err)
		return
	}

	var wg sync.WaitGroup

	for _, instance := range dbInstances {
		dbInstanceIdentifier := aws.StringValue(instance.DBInstanceIdentifier)
		wg.Add(1)
		go func(dbIdentifier string) {
			defer wg.Done()
			handleRDSInstance(cloudwatchClient, rdsClient, dbIdentifier, alarmThreshold, snsTopicArn)
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

// listALBs lists all Application Load Balancers.
func listALBs(client *elbv2.ELBV2) ([]*elbv2.LoadBalancer, error) {
	resp, err := client.DescribeLoadBalancers(&elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, err
	}
	return resp.LoadBalancers, nil
}

// handleALB creates an alarm for a given Application Load Balancer.
func handleALB(cloudwatchClient *cloudwatch.CloudWatch, loadBalancerName string, alarmThreshold float64, snsTopicArn string) {
	details := AlarmDetails{
		Namespace:          "AWS/ApplicationELB",
		MetricName:         "HTTPCode_ELB_5XX_Count",
		ResourceID:         loadBalancerName,
		DimensionNames:     []string{"LoadBalancer"},
		DimensionValues:    []string{loadBalancerName},
		ComparisonOperator: "GreaterThanThreshold",
		EvaluationPeriods:  1,
		Period:             600,
		Statistic:          "Sum",
	}

	err := createAlarm(cloudwatchClient, details, alarmThreshold, snsTopicArn)
	if err != nil {
		log.Printf("Failed to create alarm for Application Load Balancer %s: %v", loadBalancerName, err)
	}
}

// getLoadBalancerId extracts the LoadBalancer ID from the provided ARN string.
func getLoadBalancerId(arn string) string {
	parts := strings.Split(arn, "/")

	if len(parts) >= 3 {
		result := parts[len(parts)-3] + "/" + parts[len(parts)-2] + "/" + parts[len(parts)-1]
		return result
	}
	return "String format unexpected"
}

// createCloudwatchAlarmForALBs creates CloudWatch alarms for Application Load Balancers.
func createCloudwatchAlarmForALBs(sess *session.Session, alarmThreshold float64, snsTopicArn string) {
	elbv2Client := elbv2.New(sess)
	cloudwatchClient := cloudwatch.New(sess)

	loadBalancers, err := listALBs(elbv2Client)
	if err != nil {
		log.Fatalf("Failed to list Application Load Balancers: %v", err)
		return
	}

	var wg sync.WaitGroup

	for _, lb := range loadBalancers {
		loadBalancerName := getLoadBalancerId(*lb.LoadBalancerArn)

		wg.Add(1)
		go func(lbName string) {
			defer wg.Done()
			handleALB(cloudwatchClient, lbName, alarmThreshold, snsTopicArn)
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
	fmt.Println(regionName, profileName, alarmThreshold)

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
	//	createCloudwatchAlarmForEC2Instances(sess, alarmThreshold, snsTopicArn)
	//	createCloudwatchAlarmForECSServices(sess, alarmThreshold, snsTopicArn)
	//	createCloudwatchAlarmForRDSInstances(sess, alarmThreshold, snsTopicArn)
	//	createCloudwatchAlarmForALBs(sess, 5, snsTopicArn)

	var wg sync.WaitGroup

	// Create CloudWatch alarms for EC2 instances.
	wg.Add(1)
	go func() {
		defer wg.Done()
		createCloudwatchAlarmForEC2Instances(sess, alarmThreshold, snsTopicArn)
	}()

	// Create CloudWatch alarms for ECS services.
	wg.Add(1)
	go func() {
		defer wg.Done()
		createCloudwatchAlarmForECSServices(sess, alarmThreshold, snsTopicArn)
	}()

	// Create CloudWatch alarms for RDS instances.
	wg.Add(1)
	go func() {
		defer wg.Done()
		createCloudwatchAlarmForRDSInstances(sess, alarmThreshold, snsTopicArn)
	}()

	// Create CloudWatch alarms for ALBs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		createCloudwatchAlarmForALBs(sess, 5, snsTopicArn)
	}()

	// Wait for all goroutines to finish.
	wg.Wait()

}
