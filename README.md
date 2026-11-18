# Demo AI agent for Infrastructure  (Go + Local Ollama + Pulumi + LocalStack)

A local-first, guardrailed AI platform engineering agent written in **Golang**. It translates natural language infrastructure requests into safe, compliant AWS VPC topologies using **Ollama Structured Outputs**, enforces deterministic networking and security policies via **`net/netip`**, and provisions resources declaratively via the **Pulumi Automation API** against **LocalStack**.

---

## Architecture

```mermaid
flowchart TD
    User([User Prompt]) -->|Natural Language| Ollama[Local Ollama LLM]
    Ollama -->|Strict JSON Schema| Guardrail{Go Guardrail Engine}
    
    LocalStack[(LocalStack AWS API)] -.->|DescribeVpcs CIDR Check| Guardrail
    
    Guardrail -->|Blocked Risks & FinOps Fixes| Audit[Security & Cost Audit]
    Guardrail -->|ValidatedSpec| Pulumi[Pulumi Automation API]
    
    Pulumi -->|stack.Preview Diff| Gate{Human Approval y/N}
    Gate -->|Approved| Deploy[Provision Multi-Tier VPC]
    
    subgraph AWSVPC [Provisioned AWS Topology in LocalStack]
        VPC[VPC + Internet Gateway]
        PubSub[Public Subnet .1.0/24 + Conditional NAT GW]
        PrivSub[Private Subnet .2.0/24 + Stateless NACL]
        SG[Stateful Security Group + Optional EC2]
    end
    
    Deploy --> AWSVPC
    AWSVPC --> LocalStack
```
---

## Project Structure

```text
demo-agent-vpc/
├── cmd/agent/main.go                  # CLI flags, audit summary renderer, & [y/N] approval gate
├── internal/
│   ├── llm/ollama_parser.go           # Ollama /api/chat client enforcing strict JSON Schema output
│   ├── policy/
│   │   ├── guardrail.go               # Deterministic security, FinOps, & net/netip subnet carver
│   └── provisioner/
│       ├── aws_discovery.go           # Live LocalStack VPC CIDR inspection via aws-sdk-go-v2
│       └── pulumi_runner.go           # Inline Pulumi Go program with local state backend
└── Makefile                           # Convenience targets ( run, destroy)
```

---

## Deterministic Guardrails (Security, Networking & FinOps)

Every LLM output is treated as untrusted input and passed through deterministic Go policies before touching cloud APIs:

| Guardrail Category | Policy Enforced in Go | Example Automatic Remediation |
| :--- | :--- | :--- |
| **Perimeter Security** | Blocks SSH (`22`), RDP (`3389`), PostgreSQL (`5432`), and MySQL (`3306`) from Security Groups and enforces a stateless **Network ACL** explicitly denying ports `22` & `3389`. | Strips port `22` and advises using AWS SSM Session Manager instead. |
| **Live CIDR Collision** | Queries existing VPCs via `DescribeVpcs` and checks overlaps with `net/netip`. | Shifts colliding `10.10.0.0/16` automatically to the next free `/16` block (`10.11.0.0/16`). |
| **Multi-Tier Subnetting** | Carves two isolated `/24` subnets from the validated VPC prefix using `net/netip`. | Allocates `.1.0/24` for NAT egress and `.2.0/24` for private compute (`MapPublicIpOnLaunch: false`). |
| **FinOps Cost Control** | Skips Elastic IP & NAT Gateway creation when no private compute instance is deployed, and restricts EC2 sizes to `t3.nano`, `t3.micro`, or `t3.small`. | Downgrades `m5.4xlarge` to `t3.micro`; skips idle NAT Gateway on network-only runs. |
| **Human-in-the-Loop** | Executes native `stack.Preview()` before mutating state. | Displays full Pulumi resource diff and requires explicit `[y/N]` confirmation. |

---


---

### Prerequisites
* Go 1.22+
* [LocalStack](https://www.localstack.cloud/localstack-open-source)
* [Pulumi CLI](https://pulumi.com)
* [Ollama](https://ollama.com) with a local model pulled (`ollama pull qwen2.5:1.5b`)

---

### 1. Check Makefile for building Go binaries, examples, etc
```
Before using make commands, run "go mod init demo-agent-vpc" and "go mod tidy" in the root directory of the project.
In case of errors, check your go env. Make sure GO111MODULE is set to "on".
```

### Example for Makefile
#### a. Build binaries
```bash
demo-AI-Agent-Infra/demo-agent-vpc-nat$ make build
go build -o bin/demo-agent-vpc-nat ./cmd/agent
```
#### b. Deploy one of the examples with Natural Language Prompt 
```bash
demo-AI-Agent-Infra/demo-agent-vpc-nat# make demo-vpc-nat-instance
go run ./cmd/agent \
        -prompt "Deploy a VPC named single-nat-instance-lab in 10.40.0.0/16 with NAT and t3.small instance and port 2181 open"
1. Parsing intent via Ollama (qwen2.5:1.5b)...
2. Inspecting live LocalStack VPC state & running Go guardrails...

==========================================
             DEMO AGENT AND NAT
==========================================
  [ADJUSTED] Direct public IP assignment requested — overridden to private subnet (use an ALB/NAT for ingress)

--- Final Approved Topology ---
  Stack Name:    single-nat-instance-lab
  VPC CIDR:      10.40.0.0/16 (requested: 10.40.0.0/16)
  Public Subnet CIDR:   10.40.1.0/24 (public)
  Private Subnet CIDR:   10.40.2.0/24 (private)
  NAT Gateway:     true
  Instance Type: t3.small (requested: t3.small)
  Allowed Ports: [2181]
==========================================

  Instance Type:   t3.small (requested: t3.small)
  Allowed Ports:   [2181]
==========================================

3. Running Pulumi Preview against LocalStack...
Previewing update (single-nat-instance-lab):

[ ...... snip ....... ]

Outputs:
    instanceId            : "i-cf4b6d478963676d5"
    internetGatewayId     : "igw-957a413e49a379b3b"
    natGatewayId          : "nat-46014fdf7c3b7ef06"
    privateComputeSubnetId: "subnet-c0a32ae9c4e4b8eff"
    publicNatSubnetId     : "subnet-f8c84aa86bb6ada12"
    securityGroupId       : "sg-341069e919544e4a0"
    vpcId                 : "vpc-25541b9a04403f730"

Resources:
    + 14 created

Duration: 13s
```
#### c. Check new resources deployed
```bash
demo-AI-Agent-Infra/demo-agent-vpc-nat# make verify-aws
=== VPCs ===
----------------------------------------------------------------------------------------
|                                     DescribeVpcs                                     |
+---------------+------------+-------------------------------+-------------------------+
|     CIDR      | IsDefault  |             Name              |          VpcId          |
+---------------+------------+-------------------------------+-------------------------+
|  172.31.0.0/16|  True      |  None                         |  vpc-cb85bf512e2acf3d5  |
|  10.40.0.0/16 |  False     |  single-nat-instance-lab-vpc  |  vpc-25541b9a04403f730  |
+---------------+------------+-------------------------------+-------------------------+
---------------------------------------------------------------------------------------------------------
|                                            DescribeSubnets                                            |
+---------------------------+--------------------------------------------------+------------------------+
|         SubnetId          |                      Tags                        |         VpcId          |
+---------------------------+--------------------------------------------------+------------------------+
|  subnet-1ef8331f65a261f24 |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-acfe47e7f93bd157c |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-7bbf4302fdd65c8e6 |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-57c7c498e87841ef8 |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-c0a32ae9c4e4b8eff |  single-nat-instance-lab-private-compute-subnet  |  vpc-25541b9a04403f730 |
|  subnet-b2a9b0a5e6253fee9 |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-0581d5953cea4b3a8 |  None                                            |  vpc-cb85bf512e2acf3d5 |
|  subnet-f8c84aa86bb6ada12 |  single-nat-instance-lab-public-nat-subnet       |  vpc-25541b9a04403f730 |
+---------------------------+--------------------------------------------------+------------------------+
----------------------------------------------------------------------
|                       DescribeSecurityGroups                       |
+----------------------+-------------------+-------------------------+
|  sg-ba956d08623c1f342|  default          |  vpc-cb85bf512e2acf3d5  |
|  sg-b7360749a73419dec|  default          |  vpc-25541b9a04403f730  |
|  sg-341069e919544e4a0|  demo-sg-b79de25  |  vpc-25541b9a04403f730  |
+----------------------+-------------------+-------------------------+
----------------------------------------------------------------------------------
|                                DescribeInstances                               |
+-----------------------+-----------------------+--------------------------------+
|  vpc-25541b9a04403f730|  i-cf4b6d478963676d5  |  single-nat-instance-lab-node  |
+-----------------------+-----------------------+--------------------------------+
-------------------------------------------------------------------------------------------------------------------------------
|                                                     DescribeNatGateways                                                     |
+-----------------------+----------------------------------+------------+---------------------------+-------------------------+
|          ID           |              Name                |   State    |          Subnet           |          VpcId          |
+-----------------------+----------------------------------+------------+---------------------------+-------------------------+
|  nat-46014fdf7c3b7ef06|  single-nat-instance-lab-nat-gw  |  available |  subnet-f8c84aa86bb6ada12 |  vpc-25541b9a04403f730  |
+-----------------------+----------------------------------+------------+---------------------------+-------------------------+
-----------------------------------------------------------------------
|                      DescribeInternetGateways                       |
+------------------------+-------------------------------+------------+
|           ID           |             Name              |   State    |
+------------------------+-------------------------------+------------+
|  igw-957a413e49a379b3b |  single-nat-instance-lab-igw  |  available |
+------------------------+-------------------------------+------------+
```

### 2. Example Deploy via Natural Language Prompt in Terminal

```bash
$ go run ./cmd/agent -prompt "Deploy an m5.4xlarge server in 10.10.0.0/16 and open port 22 and 443"

1. Parsing intent via Ollama (qwen2.5:1.5b)...
2. Inspecting live LocalStack VPC state & running Go guardrails...

==========================================
           DEMO AGENT AND NAT          
==========================================
  [BLOCKED]  SSH (port 22) blocked — use AWS SSM Session Manager instead of open SSH
  [ADJUSTED] CIDR 10.10.0.0/16 overlapped with an existing VPC — auto-shifted to 10.11.0.0/16
  [ADJUSTED] Instance type "m5.4xlarge" exceeds policy quota — downgraded to "t3.micro"

--- Final Approved Topology ---
  Stack Name:      dev-vpc
  VPC CIDR:        10.11.0.0/16 (requested: 10.10.0.0/16)
  Public Subnet:   10.11.1.0/24 (NAT egress tier)
  Private Subnet:  10.11.2.0/24 (isolated compute tier)
  Instance Type:   t3.micro (requested: m5.4xlarge)
  NAT Gateway:     true
  Allowed Ports:   [443]
==========================================

3. Running Pulumi Preview against LocalStack...
Approve and deploy this guardrailed architecture to LocalStack? [y/N]: y
```

---


### 3. Tear Down a Stack from Terminal
```bash
go run ./cmd/agent -destroy -stack dev-vpc
```
