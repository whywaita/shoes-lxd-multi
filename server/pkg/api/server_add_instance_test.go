package api

import (
	"reflect"
	"testing"
)

func Test_getMinTargets(t *testing.T) {
	var (
		hostA  = targetHost{percentOverCommit: 1}
		hostA2 = targetHost{percentOverCommit: 1}
		hostB  = targetHost{percentOverCommit: 2}
		hostC  = targetHost{percentOverCommit: 3}
	)

	type args struct {
		hosts []targetHost
	}
	tests := []struct {
		name string
		args args
		want []targetHost
	}{
		{
			name: "Single host",
			args: args{
				hosts: []targetHost{hostA},
			},
			want: []targetHost{hostA},
		},
		{
			name: "Multiple host",
			args: args{
				hosts: []targetHost{hostA, hostB, hostC},
			},
			want: []targetHost{hostA},
		},
		{
			name: "Multiple host (random order)",
			args: args{
				hosts: []targetHost{hostC, hostA, hostB},
			},
			want: []targetHost{hostA},
		},
		{
			name: "Multiple host result",
			args: args{
				hosts: []targetHost{hostA, hostA2, hostB, hostC},
			},
			want: []targetHost{hostA, hostA2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getMinTargets(tt.args.hosts); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getMinTargets() = %v, want %v", got, tt.want)
			}
		})
	}
}
