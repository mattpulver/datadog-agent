// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2022-present Datadog, Inc.

package trigger

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/arn"
	json "github.com/json-iterator/go"

	"github.com/DataDog/datadog-agent/pkg/serverless/trigger/events"
)

// GetAWSPartitionByRegion parses an AWS region and returns an AWS partition
func GetAWSPartitionByRegion(region string) string {
	if strings.HasPrefix(region, "us-gov-") {
		return "aws-us-gov"
	} else if strings.HasPrefix(region, "cn-") {
		return "aws-cn"
	}
	return "aws"
}

// ExtractAPIGatewayEventARN returns an ARN from an APIGatewayProxyRequest
func ExtractAPIGatewayEventARN(event events.APIGatewayProxyRequest, region string) string {
	requestContext := event.RequestContext
	return fmt.Sprintf("arn:%v:apigateway:%v::/restapis/%v/stages/%v", GetAWSPartitionByRegion(region), region, requestContext.APIID, requestContext.Stage)
}

// ExtractAPIGatewayV2EventARN returns an ARN from an APIGatewayV2HTTPRequest
func ExtractAPIGatewayV2EventARN(event events.APIGatewayV2HTTPRequest, region string) string {
	requestContext := event.RequestContext
	return fmt.Sprintf("arn:%v:apigateway:%v::/restapis/%v/stages/%v", GetAWSPartitionByRegion(region), region, requestContext.APIID, requestContext.Stage)
}

// ExtractAPIGatewayWebSocketEventARN returns an ARN from an APIGatewayWebsocketProxyRequest
func ExtractAPIGatewayWebSocketEventARN(event events.APIGatewayWebsocketProxyRequest, region string) string {
	requestContext := event.RequestContext
	return fmt.Sprintf("arn:%v:apigateway:%v::/restapis/%v/stages/%v", GetAWSPartitionByRegion(region), region, requestContext.APIID, requestContext.Stage)
}

// ExtractAPIGatewayCustomAuthorizerEventARN returns an ARN from an APIGatewayCustomAuthorizerRequest
func ExtractAPIGatewayCustomAuthorizerEventARN(event events.APIGatewayCustomAuthorizerRequest) string {
	return event.MethodArn
}

// ExtractAPIGatewayCustomAuthorizerRequestTypeEventARN returns an ARN from an APIGatewayCustomAuthorizerRequestTypeRequest
func ExtractAPIGatewayCustomAuthorizerRequestTypeEventARN(event events.APIGatewayCustomAuthorizerRequestTypeRequest) string {
	return event.MethodArn
}

// ExtractAlbEventARN returns an ARN from an ALBTargetGroupRequest
func ExtractAlbEventARN(event events.ALBTargetGroupRequest) string {
	return event.RequestContext.ELB.TargetGroupArn
}

// ExtractCloudwatchEventARN returns an ARN from a CloudWatchEvent
func ExtractCloudwatchEventARN(event events.CloudWatchEvent) string {
	if len(event.Resources) == 0 {
		return ""
	}
	return event.Resources[0]
}

// ExtractCloudwatchLogsEventARN returns an ARN from a CloudwatchLogsEvent
func ExtractCloudwatchLogsEventARN(event events.CloudwatchLogsEvent, region string, accountID string) (string, error) {
	decodedLog, err := event.AWSLogs.Parse()
	if err != nil {
		return "", fmt.Errorf("Couldn't decode Cloudwatch Logs event: %v", err)
	}
	return fmt.Sprintf("arn:%v:logs:%v:%v:log-group:%v", GetAWSPartitionByRegion(region), region, accountID, decodedLog.LogGroup), nil
}

// ExtractDynamoDBStreamEventARN returns an ARN from a DynamoDBEvent
func ExtractDynamoDBStreamEventARN(event events.DynamoDBEvent) string {
	return event.Records[0].EventSourceArn
}

// ExtractKinesisStreamEventARN returns an ARN from a KinesisEvent
func ExtractKinesisStreamEventARN(event events.KinesisEvent) string {
	return event.Records[0].EventSourceArn
}

// ExtractS3EventArn returns an ARN from a S3Event
func ExtractS3EventArn(event events.S3Event) string {
	return event.Records[0].EventSource
}

// ExtractSNSEventArn returns an ARN from a SNSEvent
func ExtractSNSEventArn(event events.SNSEvent) string {
	return event.Records[0].SNS.TopicArn
}

// ExtractSQSEventARN returns an ARN from a SQSEvent
func ExtractSQSEventARN(event events.SQSEvent) string {
	return event.Records[0].EventSourceARN
}

// GetTagsFromAPIGatewayEvent returns a tagset containing http tags from an
// APIGatewayProxyRequest
func GetTagsFromAPIGatewayEvent(event events.APIGatewayProxyRequest) map[string]string {
	httpTags := make(map[string]string)
	if event.RequestContext.DomainName != "" {
		httpTags["http.url"] = event.RequestContext.DomainName
	}
	httpTags["http.url_details.path"] = event.RequestContext.Path
	httpTags["http.method"] = event.RequestContext.HTTPMethod
	if event.Resource != "" {
		httpTags["http.route"] = event.Resource
	}
	if event.Headers != nil {
		if event.Headers["Referer"] != "" {
			httpTags["http.referer"] = event.Headers["Referer"]
		}
		if ua := event.Headers["User-Agent"]; ua != "" {
			httpTags["http.useragent"] = ua
		}
	}
	return httpTags
}

// GetTagsFromAPIGatewayV2HTTPRequest returns a tagset containing http tags from an
// APIGatewayProxyRequest
func GetTagsFromAPIGatewayV2HTTPRequest(event events.APIGatewayV2HTTPRequest) map[string]string {
	httpTags := make(map[string]string)
	httpTags["http.url"] = event.RequestContext.DomainName
	httpTags["http.url_details.path"] = event.RequestContext.HTTP.Path
	httpTags["http.method"] = event.RequestContext.HTTP.Method
	if event.RouteKey != "" {
		httpTags["http.route"] = event.RouteKey
	}
	if event.Headers != nil {
		if event.Headers["Referer"] != "" {
			httpTags["http.referer"] = event.Headers["Referer"]
		}
		if ua := event.Headers["user-agent"]; ua != "" {
			httpTags["http.useragent"] = ua
		}
	}
	return httpTags
}

// GetTagsFromAPIGatewayCustomAuthorizerEvent returns a tagset containing http tags from an
// APIGatewayCustomAuthorizerRequest
func GetTagsFromAPIGatewayCustomAuthorizerEvent(event events.APIGatewayCustomAuthorizerRequest) map[string]string {
	httpTags := make(map[string]string, 2)

	if methodArn, err := arn.Parse(event.MethodArn); err == nil {
		// Format is: api-id/stage/http-verb/path...
		parts := strings.SplitN(methodArn.Resource, "/", 4)
		if len(parts) != 4 {
			return nil
		}

		httpTags["http.method"] = parts[2]
		httpTags["http.url_details.path"] = "/" + parts[3]
	}

	return httpTags
}

// GetTagsFromAPIGatewayCustomAuthorizerRequestTypeEvent returns a tagset containing http tags from an
// APIGatewayCustomAuthorizerRequestTypeRequest
func GetTagsFromAPIGatewayCustomAuthorizerRequestTypeEvent(event events.APIGatewayCustomAuthorizerRequestTypeRequest) map[string]string {
	httpTags := make(map[string]string)
	httpTags["http.url_details.path"] = event.RequestContext.Path
	httpTags["http.method"] = event.HTTPMethod
	if event.Resource != "" {
		httpTags["http.route"] = event.Resource
	}
	if referer := event.Headers["Referer"]; referer != "" {
		httpTags["http.referer"] = referer
	}
	if ua := event.Headers["User-Agent"]; ua != "" {
		httpTags["http.useragent"] = ua
	}
	return httpTags
}

// GetTagsFromALBTargetGroupRequest returns a tagset containing http tags from an
// ALBTargetGroupRequest
func GetTagsFromALBTargetGroupRequest(event events.ALBTargetGroupRequest) map[string]string {
	httpTags := make(map[string]string)
	httpTags["http.url_details.path"] = event.Path
	httpTags["http.method"] = event.HTTPMethod

	if event.Headers != nil {
		if r := event.Headers["Referer"]; r != "" {
			httpTags["http.referer"] = r
		}
		if ua := event.Headers["User-Agent"]; ua != "" {
			httpTags["http.useragent"] = ua
		}
	} else if event.MultiValueHeaders != nil {
		if r := event.MultiValueHeaders["Referer"]; len(r) > 0 && r[0] != "" {
			httpTags["http.referer"] = r[0]
		}
		if ua := event.MultiValueHeaders["User-Agent"]; len(ua) > 0 && ua[0] != "" {
			httpTags["http.useragent"] = ua[0]
		}
	}

	return httpTags
}

// GetTagsFromLambdaFunctionURLRequest returns a tagset containing http tags from a
// LambdaFunctionURLRequest
func GetTagsFromLambdaFunctionURLRequest(event events.LambdaFunctionURLRequest) map[string]string {
	httpTags := make(map[string]string)
	if event.RequestContext.DomainName != "" {
		httpTags["http.url"] = event.RequestContext.DomainName
	}
	httpTags["http.url_details.path"] = event.RequestContext.HTTP.Path
	httpTags["http.method"] = event.RequestContext.HTTP.Method
	if event.Headers != nil {
		if event.Headers["Referer"] != "" {
			httpTags["http.referer"] = event.Headers["Referer"]
		}
		if ua := event.Headers["User-Agent"]; ua != "" {
			httpTags["http.useragent"] = ua
		}
	}
	return httpTags
}

// GetStatusCodeFromHTTPResponse parses a generic payload and returns
// a status code, if it contains one. Returns an empty string if it does not,
// or an error in case of json parsing error.
func GetStatusCodeFromHTTPResponse(rawPayload []byte) (string, error) {
	var response struct {
		StatusCode interface{} `json:"statusCode"`
	}
	err := json.Unmarshal(rawPayload, &response)
	if err != nil {
		return "", err
	}

	statusCode := response.StatusCode
	if statusCode == nil {
		return "", nil
	}

	switch actual := statusCode.(type) {
	case float64:
		return strconv.FormatFloat(actual, 'f', -1, 64), nil
	case string:
		return actual, nil
	default:
		return "", fmt.Errorf("Received unknown type %T for statusCode", statusCode)
	}
}

// ParseArn parses an AWS ARN and returns the region and account
func ParseArn(arn string) (string, string, string, error) {
	arnTokens := strings.Split(arn, ":")
	if len(arnTokens) < 5 {
		return "", "", "", fmt.Errorf("Malformed arn %v provided", arn)
	}
	if len(arnTokens) >= 7 {
		return arnTokens[3], arnTokens[4], arnTokens[6], nil
	}
	return arnTokens[3], arnTokens[4], "", nil
}
