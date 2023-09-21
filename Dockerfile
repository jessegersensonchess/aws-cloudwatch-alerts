FROM golang:1.19-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Install the AWS SDK for Go
RUN go mod init cloudwatch-alerts && \
go get github.com/aws/aws-sdk-go

# Copy the local package files to the container's workspace
COPY . .

# Build your Go app
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o cloudwatch-alert-generator

#FROM alpine:3.18
#COPY --from=builder /app/cloudwatch-alert-generator /cloudwatch-alert-generator

# Command to run the binary
#ENTRYPOINT ["./cloudwatch-alert-generator"]
#CMD [ "cp", "./cloudwatch-alert-generator", "/tmp/asdf/cloudwatch-alert-generator"]
