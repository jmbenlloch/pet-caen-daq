package main

import "testing"

func TestConcentratorAddresses(t *testing.T) {
	for _, test := range []struct {
		ip, control, stream string
		invalid             bool
	}{
		{"", "192.168.50.125:19760", "192.168.50.125:19000", false},
		{"172.16.0.11", "172.16.0.11:9760", "172.16.0.11:9000", false},
		{" 10.20.30.40 ", "10.20.30.40:9760", "10.20.30.40:9000", false},
		{"::1", "[::1]:9760", "[::1]:9000", false},
		{"invalid", "", "", true},
		{"172.16.0.11:9760", "", "", true},
	} {
		t.Run(test.ip, func(t *testing.T) {
			control, stream, err := concentratorAddresses(test.ip, "192.168.50.125:19760", "192.168.50.125:19000")
			if (err != nil) != test.invalid || control != test.control || stream != test.stream {
				t.Fatalf("got %q %q %v", control, stream, err)
			}
		})
	}
}
