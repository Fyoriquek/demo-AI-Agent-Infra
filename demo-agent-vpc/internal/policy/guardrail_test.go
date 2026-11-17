package policy

import (
	"slices"
	"testing"

	"demo-agent-vpc/internal/llm"
)

func TestEvaluateAndTransform(t *testing.T) {
	tests := []struct {
		name             string
		raw              llm.RawIntent
		existingVPCs     []string
		wantStackName    string
		wantVpcCIDR      string
		wantSubnetCIDR   string
		wantInstanceType string
		wantPorts        []int
		wantMinBlocked   int
		wantMinWarnings  int
	}{
		{
			name: "Clean compliant request passes untouched",
			raw: llm.RawIntent{
				StackName:    "clean-app",
				VpcCIDR:      "10.20.0.0/16",
				InstanceType: "t3.micro",
				AllowedPorts: []int{443},
				PublicAccess: false,
			},
			existingVPCs:     []string{"172.31.0.0/16"},
			wantStackName:    "clean-app",
			wantVpcCIDR:      "10.20.0.0/16",
			wantSubnetCIDR:   "10.20.1.0/24",
			wantInstanceType: "t3.micro",
			wantPorts:        []int{443},
			wantMinBlocked:   0,
			wantMinWarnings:  0,
		},
		{
			name: "Dangerous prompt blocks SSH/DB ports and downsizes massive instance",
			raw: llm.RawIntent{
				StackName:    "Unsafe App!!",
				VpcCIDR:      "10.10.0.0/16",
				InstanceType: "m5.4xlarge",
				AllowedPorts: []int{22, 80, 443, 5432, 3389},
				PublicAccess: true,
			},
			existingVPCs:     []string{},
			wantStackName:    "unsafe-app--",
			wantVpcCIDR:      "10.10.0.0/16",
			wantSubnetCIDR:   "10.10.1.0/24",
			wantInstanceType: "t3.micro",
			wantPorts:        []int{80, 443},
			wantMinBlocked:   3,
			wantMinWarnings:  2,
		},
		{
			name: "CIDR collision automatically shifts to next free /16 block",
			raw: llm.RawIntent{
				StackName:    "overlap-test",
				VpcCIDR:      "10.10.0.0/16",
				InstanceType: "t3.small",
				AllowedPorts: []int{8080},
				PublicAccess: false,
			},
			existingVPCs:     []string{"10.10.0.0/16", "10.11.0.0/16"},
			wantStackName:    "overlap-test",
			wantVpcCIDR:      "10.12.0.0/16",
			wantSubnetCIDR:   "10.12.1.0/24",
			wantInstanceType: "t3.small",
			wantPorts:        []int{8080},
			wantMinBlocked:   0,
			wantMinWarnings:  1,
		},
		{
			name: "Public IP CIDR range rejected and reset to RFC1918 private block",
			raw: llm.RawIntent{
				StackName:    "public-cidr-hack",
				VpcCIDR:      "8.8.0.0/16",
				InstanceType: "t3.nano",
				AllowedPorts: []int{443},
				PublicAccess: false,
			},
			existingVPCs:     []string{},
			wantStackName:    "public-cidr-hack",
			wantVpcCIDR:      "10.10.0.0/16",
			wantSubnetCIDR:   "10.10.1.0/24",
			wantInstanceType: "t3.nano",
			wantPorts:        []int{443},
			wantMinBlocked:   0,
			wantMinWarnings:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSpec, gotAudit, err := EvaluateAndTransform(tc.raw, tc.existingVPCs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotSpec.StackName != tc.wantStackName {
				t.Errorf("StackName = %q, want %q", gotSpec.StackName, tc.wantStackName)
			}
			if gotSpec.VpcCIDR != tc.wantVpcCIDR {
				t.Errorf("VpcCIDR = %q, want %q", gotSpec.VpcCIDR, tc.wantVpcCIDR)
			}
			if gotSpec.SubnetCIDR != tc.wantSubnetCIDR {
				t.Errorf("SubnetCIDR = %q, want %q", gotSpec.SubnetCIDR, tc.wantSubnetCIDR)
			}
			if gotSpec.InstanceType != tc.wantInstanceType {
				t.Errorf("InstanceType = %q, want %q", gotSpec.InstanceType, tc.wantInstanceType)
			}
			if !slices.Equal(gotSpec.AllowedPorts, tc.wantPorts) {
				t.Errorf("AllowedPorts = %v, want %v", gotSpec.AllowedPorts, tc.wantPorts)
			}
			if len(gotAudit.Blocked) < tc.wantMinBlocked {
				t.Errorf("Blocked count = %d, want at least %d (%v)", len(gotAudit.Blocked), tc.wantMinBlocked, gotAudit.Blocked)
			}
			if len(gotAudit.Warnings) < tc.wantMinWarnings {
				t.Errorf("Warnings count = %d, want at least %d (%v)", len(gotAudit.Warnings), tc.wantMinWarnings, gotAudit.Warnings)
			}
		})
	}
}

