

package policy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"demo-agent-vpc/internal/llm"
	"demo-agent-vpc/internal/provisioner"
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

	subnetPrefix, err := carveSubnet(vpcPrefix, 24)
	if err != nil {
		return provisioner.ValidatedSpec{}, report, fmt.Errorf("failed to carve subnet: %w", err)
	}

	instanceType := strings.ToLower(strings.TrimSpace(raw.InstanceType))
	if !allowedInstanceTypes[instanceType] {
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
			"Direct public IP assignment requested — use an ALB/NAT for ingress")
	}

	spec := provisioner.ValidatedSpec{
		StackName:    cleanName,
		VpcCIDR:      vpcPrefix.String(),
		SubnetCIDR:   subnetPrefix.String(),
		InstanceType: instanceType,
		AllowedPorts: safePorts,
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

func carveSubnet(vpc netip.Prefix, targetBits int) (netip.Prefix, error) {
	if !vpc.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("only IPv4 VPCs are supported")
	}
	if targetBits <= vpc.Bits() || targetBits > 28 {
		return netip.Prefix{}, fmt.Errorf("cannot carve /%d subnet from /%d VPC", targetBits, vpc.Bits())
	}

	b := vpc.Addr().As4()
	if vpc.Bits() <= 16 && targetBits >= 24 {
		b[2] = 1
	}

	subnet := netip.PrefixFrom(netip.AddrFrom4(b), targetBits).Masked()
	if !vpc.Contains(subnet.Addr()) {
		return netip.Prefix{}, fmt.Errorf("carved subnet %s falls outside VPC %s", subnet, vpc)
	}

	return subnet, nil
}
