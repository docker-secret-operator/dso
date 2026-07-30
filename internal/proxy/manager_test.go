package proxy

import (
	"reflect"
	"testing"
)

func TestParseHostPorts(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  []PortMap
	}{
		{
			name:  "empty",
			label: "",
			want:  nil,
		},
		{
			name:  "single",
			label: "3306:3306",
			want:  []PortMap{{HostPort: 3306, ContainerPort: 3306}},
		},
		{
			name:  "multiple",
			label: "3306:3306,8080:80",
			want:  []PortMap{{HostPort: 3306, ContainerPort: 3306}, {HostPort: 8080, ContainerPort: 80}},
		},
		{
			name:  "whitespace is trimmed",
			label: " 3306:3306 , 8080:80 ",
			want:  []PortMap{{HostPort: 3306, ContainerPort: 3306}, {HostPort: 8080, ContainerPort: 80}},
		},
		{
			name:  "non-numeric entries are skipped",
			label: "abc:80,9000:9000",
			want:  []PortMap{{HostPort: 9000, ContainerPort: 9000}},
		},
		{
			name:  "entries without a colon are skipped",
			label: "3306,8080:80",
			want:  []PortMap{{HostPort: 8080, ContainerPort: 80}},
		},
		{
			name:  "all invalid yields nil",
			label: "bad,worse,:,",
			want:  nil,
		},
		// SEC-7 regression: dso.host_ports is attacker-influenceable (any
		// container can set its own labels), so privileged/out-of-range host
		// ports must be rejected rather than silently bound.
		{
			name:  "privileged host port is rejected",
			label: "22:22",
			want:  nil,
		},
		{
			name:  "privileged host port among valid entries is dropped, others kept",
			label: "22:22,8080:80",
			want:  []PortMap{{HostPort: 8080, ContainerPort: 80}},
		},
		{
			name:  "host port 1024 boundary is allowed",
			label: "1024:1024",
			want:  []PortMap{{HostPort: 1024, ContainerPort: 1024}},
		},
		{
			name:  "host port 1023 boundary is rejected",
			label: "1023:1023",
			want:  nil,
		},
		{
			name:  "out-of-range host port is rejected",
			label: "70000:80",
			want:  nil,
		},
		{
			name:  "out-of-range container port is rejected",
			label: "8080:70000",
			want:  nil,
		},
		{
			name:  "negative ports are rejected",
			label: "-1:80",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseHostPorts(tt.label)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseHostPorts(%q) = %#v, want %#v", tt.label, got, tt.want)
			}
		})
	}
}
