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
	PublicSubnetCIDR   string
	PrivateSubnetCIDR  string
	InstanceType string
	AllowedPorts []int
	EnableNatGateway  bool
}


func createInlineProgram(spec ValidatedSpec) pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		// 1. Create VPC
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

		// 2. Internet Gateway
		igw, err := ec2.NewInternetGateway(ctx, "demo-igw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags:  pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-igw")},
		})
		if err != nil {
			return err
		}

		// 3. Public Subnet (Hosts NAT Gateway)
		publicSubnet, err := ec2.NewSubnet(ctx, "demo-public-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String(spec.PublicSubnetCIDR),
			MapPublicIpOnLaunch: pulumi.Bool(false),
			Tags:                pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-public-nat-subnet")},
		})
		if err != nil {
			return err
		}

		// 4. Public Route Table (0.0.0.0/0 -> Internet Gateway)
		publicRT, err := ec2.NewRouteTable(ctx, "demo-public-rt", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-public-rt")},
		})
		if err != nil {
			return err
		}

		_, err = ec2.NewRouteTableAssociation(ctx, "demo-public-rta", &ec2.RouteTableAssociationArgs{
			SubnetId:     publicSubnet.ID(),
			RouteTableId: publicRT.ID(),
		})
		if err != nil {
			return err
		}

		// 5. Private Subnet (Isolated Compute Tier)
		privateSubnet, err := ec2.NewSubnet(ctx, "demo-private-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String(spec.PrivateSubnetCIDR),
			MapPublicIpOnLaunch: pulumi.Bool(false),
			Tags:                pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-private-compute-subnet")},
		})
		if err != nil {
			return err
		}

		// 6. CONDITIONAL NAT GATEWAY & PRIVATE OUTBOUND ROUTE
		// Only created if a private compute instance needs outbound access OR user explicitly requested a NAT Gateway
		if spec.EnableNatGateway {
			eip, err := ec2.NewEip(ctx, "demo-nat-eip", &ec2.EipArgs{
				Domain: pulumi.String("vpc"),
				Tags:   pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-nat-eip")},
			})
			if err != nil {
				return err
			}

			natGw, err := ec2.NewNatGateway(ctx, "demo-nat-gw", &ec2.NatGatewayArgs{
				AllocationId: eip.ID(),
				SubnetId:     publicSubnet.ID(),
				Tags:         pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-nat-gw")},
			}, pulumi.DependsOn([]pulumi.Resource{igw}))
			if err != nil {
				return err
			}

			privateRT, err := ec2.NewRouteTable(ctx, "demo-private-rt", &ec2.RouteTableArgs{
				VpcId: vpc.ID(),
				Routes: ec2.RouteTableRouteArray{
					&ec2.RouteTableRouteArgs{
						CidrBlock:    pulumi.String("0.0.0.0/0"),
						NatGatewayId: natGw.ID(),
					},
				},
				Tags: pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-private-rt")},
			})
			if err != nil {
				return err
			}

			_, err = ec2.NewRouteTableAssociation(ctx, "demo-private-rta", &ec2.RouteTableAssociationArgs{
				SubnetId:     privateSubnet.ID(),
				RouteTableId: privateRT.ID(),
			})
			if err != nil {
				return err
			}

			ctx.Export("natGatewayId", natGw.ID())
		}

		// 7. Stateless Network ACL (Defense-in-Depth Perimeter Guardrail)
		_, err = ec2.NewNetworkAcl(ctx, "demo-private-nacl", &ec2.NetworkAclArgs{
			VpcId:     vpc.ID(),
			SubnetIds: pulumi.StringArray{privateSubnet.ID()},
			Ingress: ec2.NetworkAclIngressArray{
				&ec2.NetworkAclIngressArgs{
					RuleNo:    pulumi.Int(100),
					Protocol:  pulumi.String("tcp"),
					Action:    pulumi.String("deny"),
					CidrBlock: pulumi.String("0.0.0.0/0"),
					FromPort:  pulumi.Int(22),
					ToPort:    pulumi.Int(22),
				},
				&ec2.NetworkAclIngressArgs{
					RuleNo:    pulumi.Int(110),
					Protocol:  pulumi.String("tcp"),
					Action:    pulumi.String("deny"),
					CidrBlock: pulumi.String("0.0.0.0/0"),
					FromPort:  pulumi.Int(3389),
					ToPort:    pulumi.Int(3389),
				},
				&ec2.NetworkAclIngressArgs{
					RuleNo:    pulumi.Int(200),
					Protocol:  pulumi.String("tcp"),
					Action:    pulumi.String("allow"),
					CidrBlock: pulumi.String(spec.VpcCIDR),
					FromPort:  pulumi.Int(1),
					ToPort:    pulumi.Int(65535),
				},
				&ec2.NetworkAclIngressArgs{
					RuleNo:    pulumi.Int(210),
					Protocol:  pulumi.String("tcp"),
					Action:    pulumi.String("allow"),
					CidrBlock: pulumi.String("0.0.0.0/0"),
					FromPort:  pulumi.Int(1024),
					ToPort:    pulumi.Int(65535),
				},
			},
			Egress: ec2.NetworkAclEgressArray{
				&ec2.NetworkAclEgressArgs{
					RuleNo:    pulumi.Int(100),
					Protocol:  pulumi.String("-1"),
					Action:    pulumi.String("allow"),
					CidrBlock: pulumi.String("0.0.0.0/0"),
					FromPort:  pulumi.Int(0),
					ToPort:    pulumi.Int(0),
				},
			},
			Tags: pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-private-nacl")},
		})
		if err != nil {
			return err
		}

		// 8. Stateful Security Group
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

		// 9. Conditionally Provision EC2 Instance inside the Private Subnet
		if spec.InstanceType != "" {
			instance, err := ec2.NewInstance(ctx, "demo-instance", &ec2.InstanceArgs{
				Ami:                 pulumi.String("ami-0c55b159cbfafe1f0"),
				InstanceType:        pulumi.String(spec.InstanceType),
				SubnetId:            privateSubnet.ID(),
				VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
				Tags:                pulumi.StringMap{"Name": pulumi.String(spec.StackName + "-node")},
			})
			if err != nil {
				return err
			}
			ctx.Export("instanceId", instance.ID())
		}

		// Export core networking IDs
		ctx.Export("vpcId", vpc.ID())
		ctx.Export("internetGatewayId", igw.ID())
		ctx.Export("publicNatSubnetId", publicSubnet.ID())
		ctx.Export("privateComputeSubnetId", privateSubnet.ID())
		ctx.Export("securityGroupId", sg.ID())

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
