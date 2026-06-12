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
