# Demo for AI Agent for PoC Infrastructure Deployment 
BINARY_NAME := demo-agent-vpc-nat
CMD_PATH    := ./cmd/agent
MODEL       ?= qwen2.5:1.5b
ENV         ?= dev
STACK       ?= single-nat-instance-lab

.PHONY: help test vet build demo-vpc-nat demo-vpc-nat-instance verify-aws destroy clean

help: ## Show available make targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

vet: ## Run Go static analysis and formatting check
	gofmt -s -w .
	go vet ./...

build: ## Compile the vpc-demo CLI binary
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)

demo-vpc-nat: ## Deploy Single-AZ VPC without instance but with NAT
	go run $(CMD_PATH)  \
		-prompt "Deploy a VPC named $(STACK) in 10.60.0.0/16 with NAT and port 9005 open"

demo-vpc-nat-instance: ## Deploy Single-AZ VPC with instance and NAT
	go run $(CMD_PATH) \
		-prompt "Deploy a VPC named $(STACK) in 10.40.0.0/16 with NAT and t3.small instance and port 2181 open"

verify-aws: ## Inspect active VPCs on LocalStack
	@echo "=== VPCs ==="
	@aws ec2 --endpoint-url=http://localhost:4566 describe-vpcs \
		--query 'Vpcs[*].{VpcId:VpcId, CIDR:CidrBlock, IsDefault:IsDefault,  Name:Tags[?Key==`Name`]|[0].Value}' --output table
	@aws ec2  --endpoint-url=http://localhost:4566  describe-subnets \
		--query 'Subnets[*].{SubnetId:SubnetId, VpcId:VpcId, Tags:Tags[?Key==`Name`].Value | [0]}' --output table
	@aws ec2  --endpoint-url=http://localhost:4566 describe-security-groups \
		--query 'SecurityGroups[].[GroupId,GroupName, VpcId]' --output table
	@aws ec2   --endpoint-url=http://localhost:4566 describe-instances \
		--query 'Reservations[*].Instances[*].[VpcId,  InstanceId, Tags[?Key==`Name`].Value | [0] ]' --output table
	@aws ec2  --endpoint-url=http://localhost:4566 describe-nat-gateways \
		--query 'NatGateways[*].{ID:NatGatewayId,State:State,Subnet:SubnetId,VpcId:VpcId,  Name:Tags[?Key==`Name`]|[0].Value}' \
		--output table
	@aws ec2  --endpoint-url=http://localhost:4566  describe-internet-gateways \
		--query 'InternetGateways[*].{ID:InternetGatewayId,State:Attachments[0].State, Name:Tags[?Key==`Name`]|[0].Value}' \
                --output table

destroy: ## Destroy a LocalStack stack (usage: make destroy STACK=single-lab)
	go run $(CMD_PATH) -destroy -stack $(STACK)

clean: ## Remove compiled binaries
	rm -rf bin/

