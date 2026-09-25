package build

import (
	"reflect"
	"testing"
)

func TestBuildOptionsArgs(t *testing.T) {
	tests := []struct {
		name string
		opts BuildOptions
		want []string
	}{
		{
			name: "default",
			opts: BuildOptions{},
			want: []string{"build", "-o", "bin/alpha", "./cmd/server"},
		},
		{
			name: "trimpath",
			opts: BuildOptions{TrimPath: true},
			want: []string{"build", "-o", "bin/alpha", "-trimpath", "./cmd/server"},
		},
		{
			name: "version only",
			opts: BuildOptions{Version: "v1.2.3"},
			want: []string{"build", "-o", "bin/alpha", "-ldflags", "-X main.version=v1.2.3", "./cmd/server"},
		},
		{
			name: "version, strip and user ldflags merge in order",
			opts: BuildOptions{
				Version: "v1.2.3",
				Strip:   true,
				Ldflags: []string{"-X main.commit=abc1234"},
			},
			want: []string{
				"build", "-o", "bin/alpha",
				"-ldflags", "-X main.version=v1.2.3 -s -w -X main.commit=abc1234",
				"./cmd/server",
			},
		},
		{
			name: "strip without ldflags still emits -ldflags",
			opts: BuildOptions{Strip: true},
			want: []string{"build", "-o", "bin/alpha", "-ldflags", "-s -w", "./cmd/server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.buildArgs("bin/alpha")
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("buildArgs mismatch:\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}
