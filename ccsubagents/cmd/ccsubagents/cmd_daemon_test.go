package main

import (
	"errors"
	"testing"

	"github.com/CeraCharlesCC/CCSubAgents/ccsubagents/internal/daemonclient"
)

func TestIsAlreadyStoppedRemoteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "service unavailable transport error is not idempotent",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory"},
			want: false,
		},
		{
			name: "explicit already unavailable message is idempotent",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "daemon already unavailable"},
			want: true,
		},
		{
			name: "explicit already stopped message is idempotent",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeInternal, Message: "daemon already stopped"},
			want: true,
		},
		{
			name: "unauthorized must not be swallowed",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeUnauthorized, Message: "missing or invalid token"},
			want: false,
		},
		{
			name: "plain errors are not idempotent",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAlreadyStoppedRemoteError(tc.err); got != tc.want {
				t.Fatalf("isAlreadyStoppedRemoteError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsStoppedStatusError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "missing unix socket means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial unix /tmp/ccsubagentsd.sock: connect: no such file or directory"},
			want: true,
		},
		{
			name: "connection refused means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "dial tcp 127.0.0.1:19131: connect: connection refused"},
			want: true,
		},
		{
			name: "already unavailable means stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeServiceUnavailable, Message: "daemon already unavailable"},
			want: true,
		},
		{
			name: "unauthorized must not be treated as stopped",
			err:  &daemonclient.RemoteError{Code: daemonclient.CodeUnauthorized, Message: "missing or invalid token"},
			want: false,
		},
		{
			name: "plain errors are not stopped",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStoppedStatusError(tc.err); got != tc.want {
				t.Fatalf("isStoppedStatusError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
