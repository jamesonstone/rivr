package manifest

import (
	"reflect"
	"testing"
)

func TestCommandExecutables(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		argv []string
		want []string
	}{
		{name: "direct", argv: []string{"make", "dev"}, want: []string{"make"}},
		{
			name: "env and direnv",
			argv: []string{"env", "-u", "PC_LOG_LEVEL", "ASTRO_DEV_BACKGROUND=0", "direnv", "exec", ".", "make", "dev"},
			want: []string{"env", "direnv", "make"},
		},
		{
			name: "absolute env and shell",
			argv: []string{"/usr/bin/env", "--unset=PC_LOG_LEVEL", "direnv", "exec", ".", "sh", "-c", "exec npm run dev"},
			want: []string{"/usr/bin/env", "direnv", "sh"},
		},
		{name: "env separator", argv: []string{"env", "--", "tool"}, want: []string{"env", "tool"}},
		{name: "opaque direnv", argv: []string{"direnv", "allow"}, want: []string{"direnv"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CommandExecutables(test.argv); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("CommandExecutables(%#v) = %#v, want %#v", test.argv, got, test.want)
			}
		})
	}
}
