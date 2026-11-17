package provisioner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
        "github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	//"github.com/aws/aws-sdk-go-v2/aws"
        //"github.com/aws/aws-sdk-go-v2/config"
        //"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optdestroy"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
)
// ValidatedSpec holds the sanitized infrastructure config after Go guardrails pass.
type ValidatedSpec struct {
	StackName    string
	VpcCIDR      string
	SubnetCIDR   string
	InstanceType string
	AllowedPorts []int
}

func createInlineProgram(spec ValidatedSpec) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		vpc, err := ec2.NewVpc(ctx, "demo-vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String(spec.VpcCIDR),
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name":      pulumi.String(spec.StackName + "-vpc"),
				"ManagedBy": pulumi.String("vpc-demo-agent"),
			},
		})
		if err != nil {
			return err
		}

		subnet, err := ec2.NewSubnet(ctx, "demo-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String(spec.SubnetCIDR),
			MapPublicIpOnLaunch: pulumi.Bool(false),
			Tags: pulumi.StringMap{
				"Name": pulumi.String(spec.StackName + "-private-subnet"),
			},
		})
		if err != nil {
			return err
		}

		var ingressRules ec2.SecurityGroupIngressArray
		for _, port := range spec.AllowedPorts {
			ingressRules = append(ingressRules, &ec2.SecurityGroupIngressArgs{
				Protocol:   pulumi.String("tcp"),
				FromPort:   pulumi.Int(port),
				ToPort:     pulumi.Int(port),
				CidrBlocks: pulumi.StringArray{pulumi.String(spec.VpcCIDR)},
			})
		}

		sg, err := ec2.NewSecurityGroup(ctx, "demo-sg", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("Guardrailed SG managed by local AI agent"),
			Ingress:     ingressRules,
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
		})
		if err != nil {
			return err
		}

		instance, err := ec2.NewInstance(ctx, "demo-instance", &ec2.InstanceArgs{
			Ami:                 pulumi.String("ami-0c55b159cbfafe1f0"),
			InstanceType:        pulumi.String(spec.InstanceType),
			SubnetId:            subnet.ID(),
			VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
			Tags: pulumi.StringMap{
				"Name": pulumi.String(spec.StackName + "-node"),
			},
		})
		if err != nil {
			return err
		}

		ctx.Export("vpcId", vpc.ID())
		ctx.Export("subnetId", subnet.ID())
		ctx.Export("securityGroupId", sg.ID())
		ctx.Export("instanceId", instance.ID())

		return nil
	}
}

// InitLocalStack creates or selects a Pulumi stack wired to LocalStack and a local file backend.
func InitLocalStack(ctx context.Context, spec ValidatedSpec) (auto.Stack, error) {
	stateDir, err := filepath.Abs("./.pulumi-state")
	if err != nil {
		return auto.Stack{}, err
	}
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return auto.Stack{}, err
	}

	envVars := auto.EnvVars(map[string]string{
		"PULUMI_BACKEND_URL":       "file://" + stateDir,
		"PULUMI_CONFIG_PASSPHRASE": "local-dev-passphrase",
		"AWS_ACCESS_KEY_ID":        "test",
		"AWS_SECRET_ACCESS_KEY":    "test",
		"AWS_DEFAULT_REGION":       "us-east-1",
		"AWS_ENDPOINT_URL":         "http://localhost:4566",
		"AWS_S3_USE_PATH_STYLE":    "true",
	})

	stack, err := auto.UpsertStackInlineSource(
		ctx,
		spec.StackName,
		"vpc-demo",
		createInlineProgram(spec),
		envVars,
	)
	if err != nil {
		return auto.Stack{}, fmt.Errorf("failed to upsert inline stack: %w", err)
	}

	configs := auto.ConfigMap{
		"aws:region":                    auto.ConfigValue{Value: "us-east-1"},
		"aws:skipCredentialsValidation": auto.ConfigValue{Value: "true"},
		"aws:skipMetadataApiCheck":      auto.ConfigValue{Value: "true"},
		"aws:skipRequestingAccountId":   auto.ConfigValue{Value: "true"},
		"aws:s3UsePathStyle":            auto.ConfigValue{Value: "true"},
	}
	if err := stack.SetAllConfig(ctx, configs); err != nil {
		return auto.Stack{}, fmt.Errorf("failed to set stack config: %w", err)
	}

	return stack, nil
}

// PreviewDeployment runs a native Pulumi dry-run and streams the diff to stdout.
func PreviewDeployment(ctx context.Context, stack auto.Stack) error {
	_, err := stack.Preview(ctx, optpreview.ProgressStreams(os.Stdout))
	return err
}

// ApplyDeployment executes the approved infrastructure plan against LocalStack.
func ApplyDeployment(ctx context.Context, stack auto.Stack) (auto.OutputMap, error) {
	res, err := stack.Up(ctx, optup.ProgressStreams(os.Stdout))
	if err != nil {
		return nil, err
	}
	return res.Outputs, nil
}

// DestroyDeployment tears down an existing LocalStack stack cleanly.
func DestroyDeployment(ctx context.Context, stack auto.Stack) error {
	_, err := stack.Destroy(ctx, optdestroy.ProgressStreams(os.Stdout))
	return err
}
