package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseProcfile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []Service
		wantErr bool
	}{
		{
			name:  "basic two services",
			input: "web: node server.js\nworker: node worker.js\n",
			want: []Service{
				{Name: "web", Command: "node server.js"},
				{Name: "worker", Command: "node worker.js"},
			},
		},
		{
			name: "comments and blank lines ignored",
			input: "# a comment\n\nweb: node server.js\n   \n# trailing comment\n",
			want: []Service{
				{Name: "web", Command: "node server.js"},
			},
		},
		{
			name:  "command may contain colons and shell",
			input: "db: postgres -D ./pgdata -p $PORT_DB -k /tmp && echo ok\n",
			want: []Service{
				{Name: "db", Command: "postgres -D ./pgdata -p $PORT_DB -k /tmp && echo ok"},
			},
		},
		{
			name:  "surrounding whitespace trimmed around name and command",
			input: "  web  :   node server.js   \n",
			want: []Service{
				{Name: "web", Command: "node server.js"},
			},
		},
		{
			name:  "hyphen and underscore names allowed",
			input: "web-1: a\nback_end: b\n",
			want: []Service{
				{Name: "web-1", Command: "a"},
				{Name: "back_end", Command: "b"},
			},
		},
		{
			name:    "missing colon is an error",
			input:   "web node server.js\n",
			wantErr: true,
		},
		{
			name:    "empty command is an error",
			input:   "web:   \n",
			wantErr: true,
		},
		{
			name:    "invalid name character is an error",
			input:   "we b: node server.js\n",
			wantErr: true,
		},
		{
			name:    "duplicate service name is an error",
			input:   "web: a\nweb: b\n",
			wantErr: true,
		},
		{
			name:    "empty procfile is an error",
			input:   "# only comments\n\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProcfile(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got services %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseProcfile() =\n  %+v\nwant\n  %+v", got, tt.want)
			}
		})
	}
}
