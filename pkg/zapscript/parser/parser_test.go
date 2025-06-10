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
			name:  "whitespace is trimmed in args 1",
			input: `  **trim:  a , b `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "trim", Args: []string{"a", "b"}},
				},
			},
		},
		{
			name:  "whitespace is trimmed in args 2",
			input: `  **trim:  a , b ,,   `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "trim", Args: []string{"a", "b", "", ""}},
				},
			},
		},
		{
			name:  "whitespace is trimmed in args 3",
			input: `  **trim:a, b,,`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "trim", Args: []string{"a", "b", "", ""}},
				},
			},
		},
		{
			name:  "whitespace is trimmed in args 4",
			input: `  **trim:`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "trim", Args: []string{""}},
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
					{Name: "doit", Args: []string{""}},
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
		{
			name:  "command with one advanced arg",
			input: `**example?debug=true`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "example", AdvArgs: map[string]string{"debug": "true"}},
				},
			},
		},
		{
			name:  "command with args and one advanced arg",
			input: `**download:file1.txt?verify=sha256`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "download", Args: []string{"file1.txt"}, AdvArgs: map[string]string{"verify": "sha256"}},
				},
			},
		},
		{
			name:  "command with multiple advanced args",
			input: `**launch:game.exe?platform=win&fullscreen=yes&lang=en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"game.exe"}, AdvArgs: map[string]string{
						"platform":   "win",
						"fullscreen": "yes",
						"lang":       "en",
					}},
				},
			},
		},
		{
			name:  "command with args and trailing || after adv args",
			input: `**start:demo.bin?mode=fast||`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "start", Args: []string{"demo.bin"}, AdvArgs: map[string]string{"mode": "fast"}},
				},
			},
		},
		{
			name:  "command with empty adv arg value",
			input: `**run:foo?trace=`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "run", Args: []string{"foo"}, AdvArgs: map[string]string{"trace": ""}},
				},
			},
		},
		{
			name:  "command with arg but no adv args",
			input: `**build:release`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "build", Args: []string{"release"}, AdvArgs: nil},
				},
			},
		},
		{
			name:  "escaped question mark in arg (not adv arg)",
			input: `**print:Hello\?World`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "print", Args: []string{"Hello?World"}},
				},
			},
		},
		{
			name:  "escaped ampersand in adv arg value",
			input: `**go:main?cmd=build\&run`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "go", Args: []string{"main"}, AdvArgs: map[string]string{"cmd": "build&run"}},
				},
			},
		},
		{
			name:  "advanced args only, no standard args",
			input: `**env?debug=1&trace=0`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "env", AdvArgs: map[string]string{"debug": "1", "trace": "0"}},
				},
			},
		},
		{
			name:  "adv arg missing equals sign (ignored value)",
			input: `**conf:dev?debug`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "conf", Args: []string{"dev"}, AdvArgs: map[string]string{"debug": ""}},
				},
			},
		},
		{
			name:  "adv arg with equals but no key (empty key)",
			input: `**bad:input?=oops`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "bad", Args: []string{"input"}, AdvArgs: nil},
				},
			},
		},
		{
			name:  "adv arg with multiple equals signs",
			input: `**env:prod?path=/bin:/usr/bin&cfg=foo=bar`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "env", Args: []string{"prod"}, AdvArgs: map[string]string{
						"path": "/bin:/usr/bin",
						"cfg":  "foo=bar",
					}},
				},
			},
		},
		{
			name:  "adv arg ends with ampersand",
			input: `**launch?devmode=on&`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", AdvArgs: map[string]string{
						"devmode": "on",
					}},
				},
			},
		},
		{
			name:  "adv arg starts with ampersand",
			input: `**boot?&init=1`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "boot", AdvArgs: map[string]string{
						"init": "1",
					}},
				},
			},
		},
		{
			name:  "escaped equals sign in value",
			input: `**cfg:file.cfg?env=dev\=beta`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", Args: []string{"file.cfg"}, AdvArgs: map[string]string{
						"env": "dev=beta",
					}},
				},
			},
		},
		{
			name:  "escaped ampersand in middle of value",
			input: `**test:yes?data=foo\&bar\&baz`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test", Args: []string{"yes"}, AdvArgs: map[string]string{
						"data": "foo&bar&baz",
					}},
				},
			},
		},
		{
			name:  "escaped backslash before special",
			input: `**safe:ok?path=c:\\windows\\system32`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "safe", Args: []string{"ok"}, AdvArgs: map[string]string{
						"path": `c:\windows\system32`,
					}},
				},
			},
		},
		{
			name:  "invalid spacing before ? triggers fallback",
			input: `**opt ? a = b & c = d `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"**opt "}, AdvArgs: map[string]string{
						" a ": "b",
						" c ": "d",
					}},
				},
			},
		},
		{
			name:  "only advanced args with weird spacing 2",
			input: `**opt? a = b & c = d `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "opt", AdvArgs: map[string]string{
						" a ": "b",
						" c ": "d",
					}},
				},
			},
		},
		{
			name:  "empty adv args section (just ?)",
			input: `**nada?`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "nada"},
				},
			},
		},
		{
			name:  "adv args terminated early with ||",
			input: `**snap?mode=auto||**zap`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "snap", AdvArgs: map[string]string{"mode": "auto"}},
					{Name: "zap"},
				},
			},
		},
		{
			name:  "multiple commands with messy syntax",
			input: `**alpha:one,two?x=1&y=2||**beta?z=9 `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "alpha", Args: []string{"one", "two"}, AdvArgs: map[string]string{
						"x": "1", "y": "2",
					}},
					{Name: "beta", AdvArgs: map[string]string{"z": "9"}},
				},
			},
		},
		{
			name:  "arg with dangling backslash at end",
			input: `**echo:hello\`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "echo", Args: []string{`hello\`}},
				},
			},
		},
		{
			name:  "adv arg with invalid escape in key",
			input: `**cfg?pa\th=/bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", AdvArgs: map[string]string{"pa\\th": "/bin"}},
				},
			},
		},
		{
			name:  "adv arg with dangling backslash in value",
			input: `**cfg?path=C:\bin\`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", AdvArgs: map[string]string{"path": `C:\bin\`}},
				},
			},
		},
		{
			name:  "adv arg with final backslash",
			input: `**log?file=trace\`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "log", AdvArgs: map[string]string{"file": `trace\`}},
				},
			},
		},
		{
			name:  "generic launch with windows path",
			input: `C:\games\doom\doom.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`C:\games\doom\doom.exe`}},
				},
			},
		},
		{
			name:  "generic launch with escaped pipe",
			input: `MegaDrive\|Game.bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`MegaDrive|Game.bin`}},
				},
			},
		},
		{
			name:  "generic launch with escaped question mark",
			input: `launch\?param=value`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`launch?param=value`}},
				},
			},
		},
		{
			name:  "generic launch with escaped backslashes",
			input: `path\\with\\ending\\`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`path\with\ending\`}},
				},
			},
		},
		{
			name:  "generic launch with url and query params",
			input: `https://google.com/stuff?some=args&q=something`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`https://google.com/stuff`}, AdvArgs: map[string]string{
						"some": `args`,
						"q":    `something`,
					}},
				},
			},
		},
		{
			name:  "generic launch with url and escaped query params",
			input: `https://google.com/stuff\?some=args&q=something`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`https://google.com/stuff?some=args&q=something`}},
				},
			},
		},
		{
			name:  "single quoted arg",
			input: `**say:"hello, world"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "say", Args: []string{"hello, world"}},
				},
			},
		},
		{
			name:  "multiple quoted args",
			input: `**msg:"hello, world","123"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "msg", Args: []string{"hello, world", "123"}},
				},
			},
		},
		{
			name:  "quoted with internal escaped quote",
			input: `**echo:"she said \"hello\""`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "echo", Args: []string{`she said "hello"`}},
				},
			},
		},
		{
			name:  "quoted arg with escaped backslash",
			input: `**path:"C:\\Games\\Test"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "path", Args: []string{`C:\Games\Test`}},
				},
			},
		},
		{
			name:  "quoted arg with unescaped backslash",
			input: `**path:"C:\Games\Test"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "path", Args: []string{`C:\Games\Test`}},
				},
			},
		},
		{
			name:  "quoted arg followed by unquoted",
			input: `**mix:"hello, world",next`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mix", Args: []string{"hello, world", "next"}},
				},
			},
		},
		{
			name:    "unmatched quote in arg",
			input:   `**fail:"unterminated`,
			wantErr: parser.ErrUnmatchedQuote,
		},
		{
			name:  "quoted arg with adv arg",
			input: `**cmd:"hello, world"?env=prod`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cmd", Args: []string{"hello, world"}, AdvArgs: map[string]string{"env": "prod"}},
				},
			},
		},
		{
			name:  "quoted key in adv args",
			input: `**cfg?env="prod build"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", AdvArgs: map[string]string{"env": "prod build"}},
				},
			},
		},
		{
			name:  "quoted val in adv args with escaped quote",
			input: `**cfg?note="he said \"hello\""`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", AdvArgs: map[string]string{"note": `he said "hello"`}},
				},
			},
		},
		{
			name:  "quoted generic launch path",
			input: `"MegaDrive/games/abc,def.bin"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`MegaDrive/games/abc,def.bin`}},
				},
			},
		},
		{
			name:  "quoted generic launch path with adv args",
			input: `"DOS/Games/test,123.iso"?lang=en`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`DOS/Games/test,123.iso`}, AdvArgs: map[string]string{"lang": "en"}},
				},
			},
		},
		{
			name:  "escaped and quoted together",
			input: `**weird:"hello\, world",foo\|bar`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "weird", Args: []string{"hello\\, world", "foo|bar"}},
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
