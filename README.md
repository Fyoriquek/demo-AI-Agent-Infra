# Demo AI Agent for Infrastructure  (Go + Local Ollama + Pulumi + LocalStack)

A local-first, guardrailed AI platform engineering agent written in **Golang**. It translates natural language infrastructure requests into safe, compliant AWS VPC topologies using **Ollama Structured Outputs**, enforces deterministic networking and security policies via **`net/netip`**, and provisions resources declaratively via the **Pulumi Automation API** against **LocalStack**.

Please check branches "demo-agent-vpc" (the blueprint) and "demo-agent-vpc-nat" for full architectures overview and code. 

## Homework 
The code comparison of these demo-agent-vpc and demo-agent-vpc-nat should you give an idea of what files should be changed for continuing the development of the AI Agent.
