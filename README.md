# Demo AI Agent for Infrastructure  (Go + Local Ollama + Pulumi + LocalStack)

A local-first, guardrailed AI platform engineering agent written in **Golang**. It translates natural language infrastructure requests into safe, compliant AWS VPC topologies using **Ollama Structured Outputs**, enforces deterministic networking and security policies via **`net/netip`**, and provisions resources declaratively via the **Pulumi Automation API** against **LocalStack**.

> **Why this exists:** Non-expert users can provision cloud networks via plain English without risking open administrative ports, IP CIDR collisions, or idle cloud spend—while keeping 100% of LLM inference and IaC state local.

---

#### Please check branches "demo-agent-vpc" (the blueprint) and "demo-agent-vpc-nat" for full architectures overview and code. 

---

## Homework 
1. The code comparison of these demo-agent-vpc and demo-agent-vpc-nat should you give an idea about the internals of the AI agent to continue the development for other resources to be deployed (Lambdas, S3 buckets, Multi-Availability-Zones, etc).
2. Transition of the agent to deploy on AWS Cloud.
3. What if you transition towards a full Cloud Native LLM approach, and run the agent inside the AWS Cloud (on EKS or ECS) and swap Ollama Local LLM with Amazon Bedrock?
