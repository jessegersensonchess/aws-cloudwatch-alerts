FROM golang:1.19-alpine AS builder

# Set the working directory
WORKDIR /app

# Install the AWS SDK for Go
RUN go mod init cloudwatch-alerts && \
go get github.com/aws/aws-sdk-go

# Copy the local files
COPY . .

# Build go app
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o cloudwatch-alert-generator

FROM alpine:3.18
COPY --from=builder /app/cloudwatch-alert-generator /cloudwatch-alert-generator

ENTRYPOINT ["./cloudwatch-alert-generator"]
