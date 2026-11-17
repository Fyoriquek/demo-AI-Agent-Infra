package provisioner

import (
	"context"
	"fmt"

//	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
//	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

        "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	//"github.com/pulumi/pulumi/sdk/v3/go/auto"
)

// FetchExistingVPCCIDRs queries LocalStack for all active VPC CIDR blocks.
func FetchExistingVPCCIDRs(ctx context.Context, endpointURL string) ([]string, error) {
	if endpointURL == "" {
		endpointURL = "http://localhost:4566"
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load localstack aws config: %w", err)
	}

	client := ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		o.BaseEndpoint = aws.String(endpointURL)
	})

	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to describe existing VPCs on LocalStack: %w", err)
	}

	var cidrs []string
	for _, vpc := range resp.Vpcs {
		if vpc.CidrBlock != nil && *vpc.CidrBlock != "" {
			cidrs = append(cidrs, *vpc.CidrBlock)
		}
	}

	return cidrs, nil
}


