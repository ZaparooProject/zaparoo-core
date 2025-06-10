package parser_test

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    parser.Script
		wantErr error
	}{
		{
			name:  "single command with no args",
			input: `**hello`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "hello"},
				},
			},
		},
		{
			name:  "multiple commands with no args",
			input: `**hello||**goodbye||**world`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "hello"},
					{Name: "goodbye"},
					{Name: "world"},
				},
			},
		},
		{
			name:  "single command with args",
			input: `**greet:hi,there`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "greet", Args: []string{"hi", "there"}},
				},
			},
		},
		{
			name:  "two commands separated",
			input: `**first:1,2||**second:3,4`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "first", Args: []string{"1", "2"}},
					{Name: "second", Args: []string{"3", "4"}},
				},
			},
		},
		{
			name:  "whitespace is ignored",
			input: `  **trim:  a , b `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "trim", Args: []string{"  a ", " b "}},
				},
			},
		},
		{
			name:    "missing command name",
			input:   `**:x,y`,
			wantErr: parser.ErrEmptyCmdName,
		},
		{
			name:  "invalid character in command name",
			input: `**he@llo`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`**he@llo`}},
				},
			},
		},
		{
			name:    "unexpected EOF after asterisk",
			input:   `*`,
			wantErr: parser.ErrUnexpectedEOF,
		},
		{
			name:  "command with trailing ||",
			input: `**cmd:1,2||`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cmd", Args: []string{"1", "2"}},
				},
			},
		},
		{
			name:  "command with colon but no args",
			input: `**doit:`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "doit"},
				},
			},
		},
		{
			name:  "command with single argument",
			input: `**run:onlyone`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "run", Args: []string{"onlyone"}},
				},
			},
		},
		{
			name:  "command with escaped args 1",
			input: `**test.escaped:one\,two`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one,two`}},
				},
			},
		},
		{
			name:  "command with escaped args 2",
			input: `**test.escaped:one\,two,th\|ree\|`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one,two`, `th|ree|`}},
				},
			},
		},
		{
			name:  "command with escaped args 3",
			input: `**test.escaped:one\\,two,a\\\\b`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one\`, `two`, `a\\b`}},
				},
			},
		},
		{
			name:  "generic launch 1",
			input: `DOS/some/game/to/play.iso`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`DOS/some/game/to/play.iso`}},
				},
			},
		},
		{
			name:  "generic launch 2",
			input: `/media/fat/games/DOS/some/game/to/play.iso`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`/media/fat/games/DOS/some/game/to/play.iso`}},
				},
			},
		},
		{
			name:  "generic launch 3",
			input: `C:\game\to\to\play.iso`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`C:\game\to\to\play.iso`}},
				},
			},
		},
		{
			name:  "generic launch multi 1",
			input: `C:\game\to\to\play.iso||**http.get:https://google.com/||MegaDrive/something.bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`C:\game\to\to\play.iso`}},
					{Name: "http.get", Args: []string{`https://google.com/`}},
					{Name: "launch", Args: []string{`MegaDrive/something.bin`}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parser.NewScriptReader(tt.input)
			got, err := p.Parse()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Parse() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
