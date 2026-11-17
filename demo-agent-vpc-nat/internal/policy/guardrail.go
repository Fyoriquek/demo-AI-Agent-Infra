

package policy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"demo-agent-vpc-nat/internal/llm"
	"demo-agent-vpc-nat/internal/provisioner"
)

// AuditReport tracks every intervention made by the deterministic policy engine.
type AuditReport struct {
	Warnings []string
	Blocked  []string
}

var allowedInstanceTypes = map[string]bool{
	"t3.nano":  true,
	"t3.micro": true,
	"t3.small": true,
}

var blockedPorts = map[int]string{
	22:   "SSH (port 22) blocked — use AWS SSM Session Manager instead of open SSH",
	3389: "RDP (port 3389) blocked — administrative desktop ports are prohibited",
	5432: "PostgreSQL (port 5432) blocked — database ports cannot be exposed in compute SGs",
	3306: "MySQL (port 3306) blocked — database ports cannot be exposed in compute SGs",
}

var stackNameSanitizer = regexp.MustCompile(`[^a-z0-9-]`)

// EvaluateAndTransform converts an unvalidated RawIntent into a safe ValidatedSpec.
func EvaluateAndTransform(raw llm.RawIntent, existingVPCs []string) (provisioner.ValidatedSpec, AuditReport, error) {
	var report AuditReport

	cleanName := strings.ToLower(strings.TrimSpace(raw.StackName))
	cleanName = stackNameSanitizer.ReplaceAllString(cleanName, "-")
	if cleanName == "" {
		cleanName = "demo-dev-vpc"
	}

	vpcPrefix, err := resolveSafeVPCCIDR(raw.VpcCIDR, existingVPCs, &report)
	if err != nil {
		return provisioner.ValidatedSpec{}, report, err
	}

	//subnetPrefix, err := carveSubnets(vpcPrefix)

	if err != nil {
		return provisioner.ValidatedSpec{}, report, fmt.Errorf("failed to carve subnet: %w", err)
	}

	instanceType := strings.ToLower(strings.TrimSpace(raw.InstanceType))
	if instanceType !="" && !allowedInstanceTypes[instanceType] {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Instance type %q exceeds policy quota — downgraded to \"t3.micro\"", raw.InstanceType))
		instanceType = "t3.micro"
	}

	var safePorts []int
	seenPorts := make(map[int]bool)

	for _, port := range raw.AllowedPorts {
		if port < 1 || port > 65535 {
			report.Blocked = append(report.Blocked, fmt.Sprintf("Invalid TCP port %d ignored", port))
			continue
		}
		if reason, isBlocked := blockedPorts[port]; isBlocked {
			report.Blocked = append(report.Blocked, reason)
			continue
		}
		if !seenPorts[port] {
			safePorts = append(safePorts, port)
			seenPorts[port] = true
		}
	}

	if raw.PublicAccess {
		report.Warnings = append(report.Warnings,
			"Direct public IP assignment requested — overridden to private subnet (use an ALB/NAT for ingress)")
	}

	enableNat := raw.EnableNatGateway
        if instanceType != "" && !enableNat {
                enableNat = true
                report.Warnings = append(report.Warnings,
                        "Private compute instance requested — automatically enabling NAT Gateway for outbound updates")
        } else if instanceType == "" && !enableNat {
                report.Warnings = append(report.Warnings,
                        "No compute instance requested — skipping Elastic IP & NAT Gateway to save ~$32/mo in idle AWS charges")
        }


	pubPrefix, privPrefix, err := carveSubnets(vpcPrefix)
	if err != nil {
		return provisioner.ValidatedSpec{}, report, fmt.Errorf("failed to carve subnets: %w", err)
	}

	spec := provisioner.ValidatedSpec{
		StackName:         cleanName,
		VpcCIDR:           vpcPrefix.String(),  // e.g., "10.10.0.0/16"
		PublicSubnetCIDR:  pubPrefix.String(),  // e.g., "10.10.1.0/24"
		PrivateSubnetCIDR: privPrefix.String(), // e.g., "10.10.2.0/24"
		InstanceType:      instanceType,
		AllowedPorts:      safePorts,
		EnableNatGateway:  enableNat,
	}


	return spec, report, nil
}

func resolveSafeVPCCIDR(requested string, existingVPCs []string, report *AuditReport) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(requested))
	if err != nil {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("Invalid CIDR %q supplied — defaulting to 10.10.0.0/16", requested))
		prefix = netip.MustParsePrefix("10.10.0.0/16")
	}

	prefix = prefix.Masked()

	if !prefix.Addr().IsPrivate() || prefix.Bits() < 16 || prefix.Bits() > 24 {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("CIDR %s must be a private RFC1918 block between /16 and /24 — reset to 10.10.0.0/16", prefix))
		prefix = netip.MustParsePrefix("10.10.0.0/16")
	}

	var existingPrefixes []netip.Prefix
	for _, rawExisting := range existingVPCs {
		if p, err := netip.ParsePrefix(rawExisting); err == nil {
			existingPrefixes = append(existingPrefixes, p.Masked())
		}
	}

	original := prefix
	for attempts := 0; attempts < 250; attempts++ {
		if !hasOverlap(prefix, existingPrefixes) {
			break
		}

		b := prefix.Addr().As4()
		
		b[1] = (b[1] + 1) % 255

		prefix = netip.PrefixFrom(netip.AddrFrom4(b), 16)
	}

	if prefix != original {
		report.Warnings = append(report.Warnings,
			fmt.Sprintf("CIDR %s overlapped with an existing VPC — auto-shifted to %s", original, prefix))
	}

	return prefix, nil
}

func hasOverlap(candidate netip.Prefix, existing []netip.Prefix) bool {
	for _, ep := range existing {
		if candidate.Overlaps(ep) {
			return true
		}
	}
	return false
}


// carveSubnets carves a /24 public subnet (.1.0/24) and a /24 private subnet (.2.0/24) from a /16 VPC.
func carveSubnets(vpc netip.Prefix) (publicSubnet netip.Prefix, privateSubnet netip.Prefix, err error) {
	if !vpc.Addr().Is4() {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("only IPv4 VPCs are supported")
	}
	if vpc.Bits() > 22 {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("VPC %s is too small to carve both public and private /24 subnets", vpc)
	}

	bPub := vpc.Addr().As4()
	bPub[2] = 1
	publicSubnet = netip.PrefixFrom(netip.AddrFrom4(bPub), 24).Masked()

	bPriv := vpc.Addr().As4()
	bPriv[2] = 2
	privateSubnet = netip.PrefixFrom(netip.AddrFrom4(bPriv), 24).Masked()

	if !vpc.Contains(publicSubnet.Addr()) || !vpc.Contains(privateSubnet.Addr()) {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("carved subnets fall outside VPC %s", vpc)
	}

	return publicSubnet, privateSubnet, nil
}


