package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"demo-agent-vpc-nat/internal/llm"
	"demo-agent-vpc-nat/internal/policy"
	"demo-agent-vpc-nat/internal/provisioner"
)

func main() {
	promptFlag := flag.String("prompt", "", "Natural language infrastructure request")
	destroyFlag := flag.Bool("destroy", false, "Destroy an existing stack instead of deploying")
	stackFlag := flag.String("stack", "dev-vpc", "Stack name to destroy (used with -destroy)")
	modelFlag := flag.String("model", "qwen2.5:1.5b", "Local Ollama model name")
	ollamaURLFlag := flag.String("ollama-url", "http://localhost:11434", "Local Ollama API base URL")
	localstackURLFlag := flag.String("localstack-url", "http://localhost:4566", "LocalStack endpoint URL")
	autoApproveFlag := flag.Bool("y", false, "Skip interactive confirmation prompt")
	flag.Parse()

	ctx := context.Background()

	if *destroyFlag {
		fmt.Printf("Initializing teardown for stack %q on LocalStack...\n", *stackFlag)
		dummySpec := provisioner.ValidatedSpec{StackName: *stackFlag}
		stack, err := provisioner.InitLocalStack(ctx, dummySpec)
		if err != nil {
			log.Fatalf("Failed to load stack %q: %v", *stackFlag, err)
		}

		if !*autoApproveFlag && !confirmAction(fmt.Sprintf("Destroy all resources in stack %q? [y/N]: ", *stackFlag)) {
			fmt.Println("Teardown aborted.")
			return
		}

		if err := provisioner.DestroyDeployment(ctx, stack); err != nil {
			log.Fatalf("Failed to destroy stack: %v", err)
		}
		fmt.Println("Stack destroyed cleanly.")
		return
	}

	if strings.TrimSpace(*promptFlag) == "" {
		fmt.Println("Error: please provide a -prompt or use -destroy")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("1. Parsing intent via Ollama (%s)...\n", *modelFlag)
	rawIntent, err := llm.ParsePromptToIntent(ctx, *ollamaURLFlag, *modelFlag, *promptFlag)
	if err != nil {
		log.Fatalf("Intent parsing failed: %v", err)
	}

	fmt.Println("2. Inspecting live LocalStack VPC state & running Go guardrails...")
	existingVPCs, err := provisioner.FetchExistingVPCCIDRs(ctx, *localstackURLFlag)
	if err != nil {
		log.Fatalf("Failed to query existing VPCs on LocalStack: %v", err)
	}

	spec, audit, err := policy.EvaluateAndTransform(rawIntent, existingVPCs)
	if err != nil {
		log.Fatalf("Guardrail validation failed: %v", err)
	}

	printAuditSummary(rawIntent, spec, audit)

	fmt.Println("3. Running Pulumi Preview against LocalStack...")
	stack, err := provisioner.InitLocalStack(ctx, spec)
	if err != nil {
		log.Fatalf("Failed to initialize Pulumi stack: %v", err)
	}

	if err := provisioner.PreviewDeployment(ctx, stack); err != nil {
		log.Fatalf("Pulumi preview failed: %v", err)
	}

	if !*autoApproveFlag {
		if !confirmAction("\nApprove and deploy this guardrailed architecture to LocalStack? [y/N]: ") {
			fmt.Println("Deployment canceled by user. No infrastructure was created.")
			return
		}
	}

	fmt.Println("\n4. Applying infrastructure via Pulumi Automation API...")
	outputs, err := provisioner.ApplyDeployment(ctx, stack)
	if err != nil {
		log.Fatalf("Deployment failed: %v", err)
	}

	fmt.Println("\n=== Deployment Complete ===")
	for key, val := range outputs {
		fmt.Printf("  %s: %v\n", key, val.Value)
	}
}

func printAuditSummary(raw llm.RawIntent, spec provisioner.ValidatedSpec, audit policy.AuditReport) {
	fmt.Println("\n==========================================")
	fmt.Println("             DEMO AGENT AND NAT            ")
	fmt.Println("==========================================")

	if len(audit.Blocked) == 0 && len(audit.Warnings) == 0 {
		fmt.Println("  [PASS] All requested parameters comply with security policies.")
	}

	for _, b := range audit.Blocked {
		fmt.Printf("  [BLOCKED]  %s\n", b)
	}
	for _, w := range audit.Warnings {
		fmt.Printf("  [ADJUSTED] %s\n", w)
	}

	fmt.Println("\n--- Final Approved Topology ---")
	fmt.Printf("  Stack Name:    %s\n", spec.StackName)
	fmt.Printf("  VPC CIDR:      %s (requested: %s)\n", spec.VpcCIDR, raw.VpcCIDR)
	fmt.Printf("  Public Subnet CIDR:   %s (public)\n", spec.PublicSubnetCIDR)
	fmt.Printf("  Private Subnet CIDR:   %s (private)\n", spec.PrivateSubnetCIDR)
	fmt.Printf("  NAT Gateway:     %v\n", spec.EnableNatGateway)
	fmt.Printf("  Instance Type: %s (requested: %s)\n", spec.InstanceType, raw.InstanceType)
	fmt.Printf("  Allowed Ports: %v\n", spec.AllowedPorts)
	fmt.Println("==========================================\n")
	if spec.InstanceType != "" {
                fmt.Printf("  Instance Type:   %s (requested: %s)\n", spec.InstanceType, raw.InstanceType)
        } else {
                fmt.Println("  Instance Type:   none (network-only deployment)")
        }

        fmt.Printf("  Allowed Ports:   %v\n", spec.AllowedPorts)
        fmt.Println("==========================================\n")
}

func confirmAction(promptText string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(promptText)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(input))
	return answer == "y" || answer == "yes"
}

