package gateway

import "testing"

func TestDirectTarget(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{
			name:    "plain host and port",
			address: "host.docker.internal:50051",
			want:    "passthrough:///host.docker.internal:50051",
		},
		{
			name:    "localhost",
			address: "127.0.0.1:50051",
			want:    "passthrough:///127.0.0.1:50051",
		},
		{
			name:    "explicit resolver",
			address: "dns:///gateway.internal:50051",
			want:    "dns:///gateway.internal:50051",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := directTarget(tt.address); got != tt.want {
				t.Fatalf("directTarget(%q) = %q, want %q", tt.address, got, tt.want)
			}
		})
	}
}
