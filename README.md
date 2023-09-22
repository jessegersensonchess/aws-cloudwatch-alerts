# AWS CloudWatch Alerts Generator

This tool automatically generates CloudWatch alarms for common AWS infrastructure components such as EC2 instances, ECS services, RDS instances, and Application Load Balancers (ALBs).

## Features

- Automatically detects running EC2 instances, ECS services, RDS instances, and ALBs in the specified AWS region.
- Creates CloudWatch alarms for the following metrics:
  - EC2: CPU Utilization
  - ECS: CPU and Memory Utilization
  - RDS: Free Storage Space
  - ALB: HTTP 5xx error count

## Prerequisites

- Go (to compile and run the tool)
- AWS CLI (configured with appropriate permissions)
- AWS SDK for Go (dependencies are imported in the code)

## Usage

1. Clone the repository:

```bash
git clone https://github.com/jessegersensonchess/aws-cloudwatch-alerts
cd aws-cloudwatch-alerts
```

2. Compile the tool:

```bash
go build -o cloudwatch-alerts-generator
```

3. Run the tool:

```bash
./cloudwatch-alerts-generator -region [AWS_REGION] -profile [AWS_CLI_PROFILE]
```

Replace `[AWS_REGION]` with the desired AWS region (e.g., `us-east-1`) and `[AWS_CLI_PROFILE]` with the AWS CLI profile name you wish to use.

## Docker
```
docker built -t cloudwatch-alerts-generator:latest .
docker run --rm -v ${HOME}/.aws/:/root/.aws cloudwatch-alerts-generator -region us-east-2 -profile default
```

## Flags

- `-region`: The AWS region where your resources are located. Default is `us-east-2`.
- `-profile`: The AWS CLI profile name to use for authentication. Default is `4511dev`.

## Contributing

Feel free to submit issues or pull requests if you have suggestions or improvements!



