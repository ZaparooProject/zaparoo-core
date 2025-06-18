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
			input: `**test.escaped:one^,two`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one,two`}},
				},
			},
		},
		{
			name:  "command with escaped args 2",
			input: `**test.escaped:one^,two,th^|ree^|`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one,two`, `th|ree|`}},
				},
			},
		},
		{
			name:  "command with escaped args 3",
			input: `**test.escaped:one^^,two,a^^^^b`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test.escaped", Args: []string{`one^`, `two`, `a^^b`}},
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
			input: `**print:Hello^?World`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "print", Args: []string{"Hello?World"}},
				},
			},
		},
		{
			name:  "escaped ampersand in adv arg value",
			input: `**go:main?cmd=build^&run`,
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
			input: `**cfg:file.cfg?env=dev^=beta`,
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
			input: `**test:yes?data=foo^&bar^&baz`,
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
			input: `**safe:ok?path=c:^\windows^\system32`,
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
					{Name: "launch", Args: []string{"**opt ? a = b & c = d"}},
				},
			},
		},
		{
			name:  "only advanced args with invalid spacing 2",
			input: `**opt? a = b & c = d `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "opt"},
				},
			},
		},
		{
			name:  "only advanced args with invalid spacing 3",
			input: `**opt:? a = b & c = d `,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "opt", Args: []string{"? a = b & c = d"}},
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
			name:  "adv arg with invalid escape in key 1",
			input: `**cfg?pa\th=/bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg"},
				},
			},
		},
		{
			name:  "adv arg with invalid escape in key 2",
			input: `**cfg:?pa\th=/bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", Args: []string{"?pa\\th=/bin"}},
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
			input: `MegaDrive^|Game.bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`MegaDrive|Game.bin`}},
				},
			},
		},
		{
			name:  "generic launch with escaped question mark",
			input: `launch^?param=value`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`launch?param=value`}},
				},
			},
		},
		{
			name:  "generic launch with escaped backslashes",
			input: `path^\with^\ending^\`,
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
			input: `https://google.com/stuff^?some=args&q=something`,
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
			name:  "quoted with internal quotes",
			input: `**echo:"she said "hello""`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "echo", Args: []string{`she said hello""`}},
				},
			},
		},
		{
			name:  "quoted arg with escaped backslash",
			input: `**path:"C:^\Games^\Test"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "path", Args: []string{`C:^\Games^\Test`}},
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
			input: `**cfg?note="he said "hello""`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", AdvArgs: map[string]string{"note": `he said hello""`}},
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
			input: `**weird:"hello^, world",foo^|bar`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "weird", Args: []string{"hello^, world", "foo|bar"}},
				},
			},
		},
		{
			name:  "quoted and escaped mix",
			input: `**mix:"a^,b",c^,d,e^|f,"g^"h"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mix", Args: []string{"a^,b", "c,d", "e|f", `g^h"`}},
				},
			},
		},
		{
			name:  "json-like but not json",
			input: `**cfg:map:{key=value}?debug=true`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "cfg", Args: []string{"map:{key=value}"}, AdvArgs: map[string]string{"debug": "true"}},
				},
			},
		},
		{
			name:  "escaped trailing special chars",
			input: `**data:end^,^|^?^^`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "data", Args: []string{"end,|?^"}},
				},
			},
		},
		{
			name:  "run http.get",
			input: `**http.get:https://zapa.roo/stuff^?id=5`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.get", Args: []string{`https://zapa.roo/stuff?id=5`}},
				},
			},
		},
		{
			name:  "template-ish content",
			input: `**render:level-[[difficulty]]?fx=true`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "render", Args: []string{"level-[[difficulty]]"}, AdvArgs: map[string]string{"fx": "true"}},
				},
			},
		},
		{
			name:  "quoted value with escaped quote and equals",
			input: `**info?text="he said ^"foo=bar^""`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "info", AdvArgs: map[string]string{
						"text": `he said ^foo=bar""`,
					}},
				},
			},
		},
		{
			name:  "adv args with json-like garbage",
			input: `**launch:game.exe?{bad}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"game.exe?{bad}"}},
				},
			},
		},
		{
			name:    "empty script",
			input:   "",
			want:    parser.Script{},
			wantErr: parser.ErrEmptyZapScript,
		},
		{
			name:    "empty command name",
			input:   "**",
			want:    parser.Script{},
			wantErr: parser.ErrEmptyCmdName,
		},
		{
			name:  "advanced args only",
			input: `**config?key1=val1&key2=val2`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "config",
						AdvArgs: map[string]string{
							"key1": "val1",
							"key2": "val2",
						},
					},
				},
			},
		},
		{
			name:  "mixed args and advanced args",
			input: `**do:foo,bar?flag=on&mode=test`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "do",
						Args:    []string{"foo", "bar"},
						AdvArgs: map[string]string{"flag": "on", "mode": "test"},
					},
				},
			},
		},
		{
			name:  "quoted argument with comma",
			input: `**echo:"foo,bar",baz`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "echo",
						Args: []string{`foo,bar`, "baz"},
					},
				},
			},
		},
		{
			name:    "unmatched quote in argument",
			input:   `**bad:"abc,def`,
			want:    parser.Script{},
			wantErr: parser.ErrUnmatchedQuote,
		},
		{
			name:  "command with escape sequences",
			input: `**say:hello^|world`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "say",
						Args: []string{"hello|world"},
					},
				},
			},
		},
		{
			name:  "multiple commands with mix of args and adv args",
			input: `**a:1,2?x=1&y=2||**b?f=ok||**c`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name:    "a",
						Args:    []string{"1", "2"},
						AdvArgs: map[string]string{"x": "1", "y": "2"},
					},
					{
						Name:    "b",
						AdvArgs: map[string]string{"f": "ok"},
					},
					{
						Name: "c",
					},
				},
			},
		},
		{
			name:  "generic launch with bad name",
			input: `C:\some\path\100^ completed\file.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch",
						Args: []string{"C:\\some\\path\\100 completed\\file.exe"},
					},
				},
			},
		},
		{
			name:  "generic launch with escaped bad name",
			input: `C:\some\path\100^^ completed\file.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch",
						Args: []string{"C:\\some\\path\\100^ completed\\file.exe"},
					},
				},
			},
		},
		{
			name:  "generic launch with escaped bad name",
			input: `C:\some\path\200^^ completed\file.exe||C:\some 200^^\path\100^^ completed\file.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{
						Name: "launch",
						Args: []string{"C:\\some\\path\\200^ completed\\file.exe"},
					},
					{
						Name: "launch",
						Args: []string{"C:\\some 200^\\path\\100^ completed\\file.exe"},
					},
				},
			},
		},
		{
			name:  "simple json argument",
			input: `**config:{"key": "value"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "config", Args: []string{`{"key":"value"}`}},
				},
			},
		},
		{
			name:  "json argument with multiple fields",
			input: `**setup:{"name": "test", "count": 42, "enabled": true}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "setup", Args: []string{`{"count":42,"enabled":true,"name":"test"}`}},
				},
			},
		},
		{
			name:  "nested json argument",
			input: `**deploy:{"config": {"env": "prod", "replicas": 3}, "name": "app"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "deploy", Args: []string{`{"config":{"env":"prod","replicas":3},"name":"app"}`}},
				},
			},
		},
		{
			name:  "json argument with array",
			input: `**process:{"items": [1, 2, 3], "type": "batch"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "process", Args: []string{`{"items":[1,2,3],"type":"batch"}`}},
				},
			},
		},
		{
			name:  "json argument with escaped quotes",
			input: `**message:{"text": "he said \"hello\"", "sender": "user"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "message", Args: []string{`{"sender":"user","text":"he said \"hello\""}`}},
				},
			},
		},
		{
			name:  "json argument mixed with regular args",
			input: `**run:{"port": 8080, "host": "localhost"},normal_arg`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "run", Args: []string{`{"host":"localhost","port":8080}`, "normal_arg"}},
				},
			},
		},
		{
			name:  "json argument with advanced args",
			input: `**api:{"endpoint": "/users", "method": "GET"}?timeout=30`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "api", Args: []string{`{"endpoint":"/users","method":"GET"}`}, AdvArgs: map[string]string{"timeout": "30"}},
				},
			},
		},
		{
			name:  "json in advanced arg value",
			input: `**configure?data={"debug": true, "level": "info"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "configure", AdvArgs: map[string]string{"data": `{"debug":true,"level":"info"}`}},
				},
			},
		},
		{
			name:  "multiple json arguments",
			input: `**merge:{"a": 1},{"b": 2}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "merge", Args: []string{`{"a":1}`, `{"b":2}`}},
				},
			},
		},
		{
			name:    "start script with json argument",
			input:   `{"game": "chess", "difficulty": "hard"}`,
			wantErr: parser.ErrInvalidJSON,
		},
		{
			name:    "invalid json argument - missing closing brace",
			input:   `**config:{"key": "value"`,
			wantErr: parser.ErrInvalidJSON,
		},
		{
			name:    "invalid json argument - malformed",
			input:   `**config:{"key": value}`,
			wantErr: parser.ErrInvalidJSON,
		},
		{
			name:  "empty json object",
			input: `**init:{}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "init", Args: []string{`{}`}},
				},
			},
		},
		{
			name:  "json with complex nested structure",
			input: `**complex:{"users": [{"id": 1, "meta": {"active": true}}], "total": 1}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "complex", Args: []string{`{"total":1,"users":[{"id":1,"meta":{"active":true}}]}`}},
				},
			},
		},
		{
			name:  "auto launch with normally invalid chars",
			input: `C:\some\path\completed?\usa, games/file.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`C:\some\path\completed?\usa, games/file.exe`}},
				},
			},
		},
		{
			name:  "single quotes 1",
			input: `'C:\some\path\completed?\usa, games/file.exe'`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`C:\some\path\completed?\usa, games/file.exe`}},
				},
			},
		},
		{
			name:  "single quotes 2",
			input: `**stuff:'C:\some\path\completed?\usa, games/file.exe'?doot=doot`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "stuff", Args: []string{`C:\some\path\completed?\usa, games/file.exe`}, AdvArgs: map[string]string{
						"doot": "doot",
					}},
				},
			},
		},
		{
			name:  "single quotes 3",
			input: `**stuff:^'C:\some\path\completed?\usa, games/file.exe'?doot=doot`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "stuff", Args: []string{`'C:\some\path\completed?\usa`, `games/file.exe'`}, AdvArgs: map[string]string{
						"doot": "doot",
					}},
				},
			},
		},
		{
			name:    "single quotes 4",
			input:   `**stuff:'C:\some\path\completed?\usa, games/file.exe?doot=doot`,
			wantErr: parser.ErrUnmatchedQuote,
		},
		{
			name:  "input.keyboard basic characters",
			input: `**input.keyboard:abcXYZ123`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"a", "b", "c", "X", "Y", "Z", "1", "2", "3",
					}},
				},
			},
		},
		{
			name:  "input.keyboard basic characters and auto launch",
			input: `**input.keyboard:abcXYZ123||/testing/test.bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"a", "b", "c", "X", "Y", "Z", "1", "2", "3",
					}},
					{Name: "launch", Args: []string{"/testing/test.bin"}},
				},
			},
		},
		{
			name:  "input.keyboard special characters",
			input: `**input.keyboard:!@#$%^&*()_+-=`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"!", "@", "#", "$", "%", "^", "&", "*", "(", ")", "_", "+", "-", "=",
					}},
				},
			},
		},
		{
			name:  "input.keyboard escape sequences 1",
			input: `**input.keyboard:{enter}{esc}{tab}{backspace}{space}{up}{down}{left}{right}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"{enter}", "{esc}", "{tab}", "{backspace}", "{space}",
						"{up}", "{down}", "{left}", "{right}",
					}},
				},
			},
		},
		{
			name:  "input.keyboard escape sequences 2",
			input: `**input.keyboard:\{enter}{esc}{tab}{backspace}{space}\{up\}{down}{left}{right}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"{", "e", "n", "t", "e", "r", "}",
						"{esc}", "{tab}", "{backspace}", "{space}",
						"{", "u", "p", "}",
						"{down}", "{left}", "{right}",
					}},
				},
			},
		},
		{
			name:  "input.keyboard mixed content",
			input: `**input.keyboard:Hello{space}World!{enter}123{backspace}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"H", "e", "l", "l", "o",
						"{space}",
						"W", "o", "r", "l", "d", "!",
						"{enter}",
						"1", "2", "3",
						"{backspace}",
					}},
				},
			},
		},
		{
			name:  "input.gamepad basic directions",
			input: `**input.gamepad:^^VV<><>`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad", Args: []string{
						"^", "^", "V", "V", "<", ">", "<", ">",
					}},
				},
			},
		},
		{
			name:  "input.gamepad buttons",
			input: `**input.gamepad:ABXYRLZC`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad", Args: []string{
						"A", "B", "X", "Y", "R", "L", "Z", "C",
					}},
				},
			},
		},
		{
			name:  "input.gamepad special buttons",
			input: `**input.gamepad:{start}{select}{mode}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad", Args: []string{
						"{start}", "{select}", "{mode}",
					}},
				},
			},
		},
		{
			name:  "input.gamepad complex sequence",
			input: `**input.gamepad:^^VV<><>BA{start}XY{select}RL`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad", Args: []string{
						"^", "^", "V", "V", "<", ">", "<", ">",
						"B", "A", "{start}", "X", "Y", "{select}", "R", "L",
					}},
				},
			},
		},
		{
			name:  "input.keyboard with advanced args",
			input: `**input.keyboard:Hello{enter}World?delay=100`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard",
						Args: []string{
							"H", "e", "l", "l", "o",
							"{enter}",
							"W", "o", "r", "l", "d",
						},
						AdvArgs: map[string]string{"delay": "100"}},
				},
			},
		},
		{
			name:  "input.keyboard with advanced args escaped",
			input: `**input.keyboard:Hello{enter}World\?delay=1\\00`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard",
						Args: []string{
							"H", "e", "l", "l", "o",
							"{enter}",
							"W", "o", "r", "l", "d",
							"?", "d", "e", "l", "a", "y", "=", "1", "\\", "0", "0",
						},
					},
				},
			},
		},
		{
			name:  "input.gamepad with advanced args",
			input: `**input.gamepad:AB{start}XY?repeat=2&interval=500`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad",
						Args: []string{
							"A", "B", "{start}", "X", "Y",
						},
						AdvArgs: map[string]string{
							"repeat":   "2",
							"interval": "500",
						}},
				},
			},
		},
		{
			name:  "ignore 1 star",
			input: `*testtest/ASFd/sfasafsfd.bin`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`*testtest/ASFd/sfasafsfd.bin`}},
				},
			},
		},
		{
			name:  "ignore 1 pipe",
			input: `*testtest/ASFd/sfasafsfd.bin|otherstuff`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`*testtest/ASFd/sfasafsfd.bin|otherstuff`}},
				},
			},
		},
		{
			name:  "docs example 1",
			input: `**launch:PCEngine/Another Game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`PCEngine/Another Game`}},
				},
			},
		},
		{
			name:  "docs example 2",
			input: `PCEngine/Another Game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`PCEngine/Another Game`}},
				},
			},
		},
		{
			name:  "docs example 3",
			input: `**delay:1000||PCEngine/Another Game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "delay", Args: []string{`1000`}},
					{Name: "launch", Args: []string{`PCEngine/Another Game`}},
				},
			},
		},
		{
			name:  "docs example 4",
			input: `**http.get:https://api.example.com/hello||**launch.random:Genesis`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.get", Args: []string{`https://api.example.com/hello`}},
					{Name: "launch.random", Args: []string{`Genesis`}},
				},
			},
		},
		{
			name:  "docs example 4 quotes",
			input: `**http.get:"https://api.example.com/hello?stuff=thing"?other=stuff||**launch.random:Genesis`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.get", Args: []string{`https://api.example.com/hello?stuff=thing`},
						AdvArgs: map[string]string{"other": "stuff"}},
					{Name: "launch.random", Args: []string{`Genesis`}},
				},
			},
		},
		{
			name:  "http docs example 1",
			input: `**http.get:https://example.com`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.get", Args: []string{`https://example.com`}},
				},
			},
		},
		{
			name:  "http docs example 2",
			input: `**http.get:https://example.com||_Console/SNES`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.get", Args: []string{`https://example.com`}},
					{Name: "launch", Args: []string{`_Console/SNES`}},
				},
			},
		},
		{
			name:  "http docs example 3",
			input: `**http.post:https://example.com,application/json,{"key":"value"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.post", Args: []string{
						`https://example.com`,
						`application/json`,
						`{"key":"value"}`,
					}},
				},
			},
		},
		{
			name:  "http docs example 4",
			input: `**http.post:http://localhost:8182/api/scripts/launch/update_all.sh,application/json,`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "http.post", Args: []string{
						`http://localhost:8182/api/scripts/launch/update_all.sh`,
						`application/json`,
						``,
					}},
				},
			},
		},
		{
			name:  "input docs example 1",
			input: `**input.keyboard:@`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{`@`}},
				},
			},
		},
		{
			name:  "input docs example 2",
			input: `**input.keyboard:qWeRty{enter}{up}aaa`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.keyboard", Args: []string{
						"q", "W", "e", "R", "t", "y",
						"{enter}",
						"{up}",
						"a", "a", "a",
					}},
				},
			},
		},
		{
			name:  "input docs example 3",
			input: `**input.gamepad:^^VV<><>BA{start}{select}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.gamepad", Args: []string{
						"^", "^", "V", "V", "<", ">", "<", ">", "B", "A",
						"{start}",
						"{select}",
					}},
				},
			},
		},
		{
			name:  "input docs example 4",
			input: `**input.coinp1:1`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.coinp1", Args: []string{`1`}},
				},
			},
		},
		{
			name:  "input docs example 5",
			input: `**input.coinp2:3`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "input.coinp2", Args: []string{`3`}},
				},
			},
		},
		{
			name:  "launch docs example 1",
			input: `/media/fat/games/Genesis/1 US - Q-Z/Some Game (USA, Europe).md`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`/media/fat/games/Genesis/1 US - Q-Z/Some Game (USA, Europe).md`}},
				},
			},
		},
		{
			name:  "launch docs example 2",
			input: `Genesis/1 US - Q-Z/Some Game (USA, Europe).md?launcher=LLAPIMegaDrive`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`Genesis/1 US - Q-Z/Some Game (USA, Europe).md`},
						AdvArgs: map[string]string{"launcher": "LLAPIMegaDrive"}},
				},
			},
		},
		{
			name:  "launch docs example 3",
			input: `N64/1 US - A-M/Another Game (USA).z64`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`N64/1 US - A-M/Another Game (USA).z64`}},
				},
			},
		},
		{
			name:  "launch docs example 4",
			input: `TurboGrafx16/Another Game (USA).pce`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`TurboGrafx16/Another Game (USA).pce`}},
				},
			},
		},
		{
			name:  "launch docs example 5",
			input: `tgfx16/Another Game (USA).pce`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`tgfx16/Another Game (USA).pce`}},
				},
			},
		},
		{
			name:  "launch docs example 6",
			input: `PCEngine/Another Game (USA).pce`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`PCEngine/Another Game (USA).pce`}},
				},
			},
		},
		{
			name:  "launch docs example 7",
			input: `/media/fat/games/Genesis/1 US - Q-Z/Some Game (USA, Europe).md`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`/media/fat/games/Genesis/1 US - Q-Z/Some Game (USA, Europe).md`}},
				},
			},
		},
		{
			name:  "launch docs example 8",
			input: `Genesis/1 US - Q-Z/Some Game (USA, Europe).md`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`Genesis/1 US - Q-Z/Some Game (USA, Europe).md`}},
				},
			},
		},
		{
			name:  "launch docs example 9",
			input: `_Arcade/Some Arcade Game.mra`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`_Arcade/Some Arcade Game.mra`}},
				},
			},
		},
		{
			name:  "launch docs example 10",
			input: `_@Favorites/My Favorite Game.mgl`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`_@Favorites/My Favorite Game.mgl`}},
				},
			},
		},
		{
			name:  "launch docs example 11",
			input: `Genesis/@Genesis - 2022-05-18.zip/1 US - Q-Z/Some Game (USA, Europe).md`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`Genesis/@Genesis - 2022-05-18.zip/1 US - Q-Z/Some Game (USA, Europe).md`}},
				},
			},
		},
		{
			name:  "launch docs example 12",
			input: `PCEngine/Another Game`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`PCEngine/Another Game`}},
				},
			},
		},
		{
			name:  "launch docs example 13",
			input: `N64/Some Game (USA)`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`N64/Some Game (USA)`}},
				},
			},
		},
		{
			name:  "launch docs example 14",
			input: `**launch.system:Atari2600`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.system", Args: []string{`Atari2600`}},
				},
			},
		},
		{
			name:  "launch docs example 15",
			input: `**launch.system:WonderSwanColor`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.system", Args: []string{`WonderSwanColor`}},
				},
			},
		},
		{
			name:  "launch docs example 16",
			input: `**launch.random:snes`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.random", Args: []string{`snes`}},
				},
			},
		},
		{
			name:  "launch docs example 17",
			input: `**launch.random:snes,nes`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.random", Args: []string{`snes`, `nes`}},
				},
			},
		},
		{
			name:  "launch docs example 18",
			input: `**launch.random:/media/fat/_#Favorites`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.random", Args: []string{`/media/fat/_#Favorites`}},
				},
			},
		},
		{
			name:  "launch docs example 19",
			input: `**launch.random:Genesis/*sonic*`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.random", Args: []string{`Genesis/*sonic*`}},
				},
			},
		},
		{
			name:  "launch docs example 20",
			input: `**launch.random:all/*mario*`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.random", Args: []string{`all/*mario*`}},
				},
			},
		},
		{
			name:  "launch docs example 21",
			input: `**launch.search:SNES/*mario*`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.search", Args: []string{`SNES/*mario*`}},
				},
			},
		},
		{
			name:  "launch docs example 22",
			input: `**launch.search:SNES/super mario*(*usa*`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch.search", Args: []string{`SNES/super mario*(*usa*`}},
				},
			},
		},
		{
			name:  "mister docs example 1",
			input: `**mister.ini:1`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mister.ini", Args: []string{`1`}},
				},
			},
		},
		{
			name:  "mister docs example 2",
			input: `**mister.core:_Console/SNES`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mister.core", Args: []string{`_Console/SNES`}},
				},
			},
		},
		{
			name:  "mister docs example 3",
			input: `**mister.core:_Console/PSX_20220518`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mister.core", Args: []string{`_Console/PSX_20220518`}},
				},
			},
		},
		{
			name:  "mister docs example 4",
			input: `**mister.script:update_all.sh`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mister.script", Args: []string{`update_all.sh`}},
				},
			},
		},
		{
			name:  "mister docs example 5",
			input: `**mister.script:update_all.sh?hidden=yes`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "mister.script", Args: []string{`update_all.sh`},
						AdvArgs: map[string]string{"hidden": "yes"}},
				},
			},
		},
		{
			name:  "playlist docs example 1",
			input: `**playlist.play:/media/fat/games/Genesis`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.play", Args: []string{`/media/fat/games/Genesis`}},
				},
			},
		},
		{
			name:  "playlist docs example 2",
			input: `**playlist.play:/media/fat/playlist.pls`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.play", Args: []string{`/media/fat/playlist.pls`}},
				},
			},
		},
		{
			name:  "playlist docs example 3",
			input: `**playlist.load:/media/fat/playlist.pls`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.load", Args: []string{`/media/fat/playlist.pls`}},
				},
			},
		},
		{
			name:  "playlist docs example 4",
			input: `**playlist.load:/media/fat/playlist.pls?mode=shuffle`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.load", Args: []string{`/media/fat/playlist.pls`}, AdvArgs: map[string]string{"mode": "shuffle"}},
				},
			},
		},
		{
			name:  "playlist docs example 5",
			input: `**playlist.play:/media/fat/_@Favorites`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.play", Args: []string{`/media/fat/_@Favorites`}},
				},
			},
		},
		{
			name:  "playlist docs example 6",
			input: `**playlist.play`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.play"},
				},
			},
		},
		{
			name:  "playlist docs example 7",
			input: `**playlist.stop`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.stop"},
				},
			},
		},
		{
			name:  "playlist docs example 8",
			input: `**playlist.next`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.next"},
				},
			},
		},
		{
			name:  "playlist docs example 9",
			input: `**playlist.previous`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.previous"},
				},
			},
		},
		{
			name:  "playlist docs example 10",
			input: `**playlist.pause`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.pause"},
				},
			},
		},
		{
			name:  "playlist docs example 11",
			input: `**playlist.goto:2`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.goto", Args: []string{`2`}},
				},
			},
		},
		{
			name:  "playlist docs example 12",
			input: `**playlist.open:/media/fat/playlist.pls`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "playlist.open", Args: []string{`/media/fat/playlist.pls`}},
				},
			},
		},
		{
			name:  "stop command",
			input: `**stop`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "stop"},
				},
			},
		},
		{
			name:  "execute command",
			input: `**execute:reboot`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "execute", Args: []string{`reboot`}},
				},
			},
		},
		{
			name:  "delay command",
			input: `**delay:500`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "delay", Args: []string{`500`}},
				},
			},
		},
		{
			name:  "delay combined with other commands",
			input: `_Console/SNES||**delay:10000||**input.key:88`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{`_Console/SNES`}},
					{Name: "delay", Args: []string{`10000`}},
					{Name: "input.key", Args: []string{`88`}},
				},
			},
		},
		{
			name:  "simple expression in arg",
			input: `**greet:Hello [[name]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "greet", Args: []string{"Hello [[name]]"}},
				},
			},
		},
		{
			name:  "expression at start of arg",
			input: `**load:[[filename]].exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "load", Args: []string{"[[filename]].exe"}},
				},
			},
		},
		{
			name:  "expression at end of arg",
			input: `**save:backup-[[timestamp]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "save", Args: []string{"backup-[[timestamp]]"}},
				},
			},
		},
		{
			name:  "multiple expressions in single arg",
			input: `**config:[[env]]-[[version]]-[[build]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "config", Args: []string{"[[env]]-[[version]]-[[build]]"}},
				},
			},
		},
		{
			name:  "expression in multiple args",
			input: `**connect:[[host]],[[port]],[[user]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "connect", Args: []string{"[[host]]", "[[port]]", "[[user]]"}},
				},
			},
		},
		{
			name:  "expression in advanced arg value",
			input: `**launch:game.exe?platform=[[system]]&debug=[[debug_mode]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"game.exe"}, AdvArgs: map[string]string{
						"platform": "[[system]]",
						"debug":    "[[debug_mode]]",
					}},
				},
			},
		},
		{
			name:  "expression with dots and underscores",
			input: `**deploy:[[app.name]],[[build_number]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "deploy", Args: []string{"[[app.name]]", "[[build_number]]"}},
				},
			},
		},
		{
			name:  "expression with numbers",
			input: `**level:[[level1]],[[player2_score]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "level", Args: []string{"[[level1]]", "[[player2_score]]"}},
				},
			},
		},
		{
			name:  "expression in quoted arg",
			input: `**say:"Hello [[user]], welcome to [[game]]"`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "say", Args: []string{"Hello [[user]], welcome to [[game]]"}},
				},
			},
		},
		{
			name:  "expression with escaped characters around it",
			input: `**path:C:^\Games^\[[system]]^\game.exe`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "path", Args: []string{`C:\Games\[[system]]\game.exe`}},
				},
			},
		},
		{
			name:  "complex expression with special chars",
			input: `**url:https://api.example.com/[[endpoint]]?key=[[api_key]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "url", Args: []string{"https://api.example.com/[[endpoint]]"}, AdvArgs: map[string]string{
						"key": "[[api_key]]",
					}},
				},
			},
		},
		{
			name:  "expression in JSON-like arg",
			input: `**config:{"user": "[[username]]", "env": "[[environment]]"}`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "config", Args: []string{`{"env":"[[environment]]","user":"[[username]]"}`}},
				},
			},
		},
		{
			name:  "expression in generic launch",
			input: `[[system]]/games/[[game_name]].rom`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "launch", Args: []string{"[[system]]/games/[[game_name]].rom"}},
				},
			},
		},
		{
			name:  "empty expression",
			input: `**test:prefix[[]]suffix`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test", Args: []string{"prefix[[]]suffix"}},
				},
			},
		},
		{
			name:  "expression with spaces inside",
			input: `**format:[[first name]] [[last name]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "format", Args: []string{"[[first name]] [[last name]]"}},
				},
			},
		},
		{
			name:  "expression mixed with other features",
			input: `**run:"[[app_path]]",arg2?env=[[environment]]&debug=[[debug]]||**cleanup:[[temp_dir]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "run", Args: []string{"[[app_path]]", "arg2"}, AdvArgs: map[string]string{
						"env":   "[[environment]]",
						"debug": "[[debug]]",
					}},
					{Name: "cleanup", Args: []string{"[[temp_dir]]"}},
				},
			},
		},
		{
			name:    "unmatched expression - missing closing bracket",
			input:   `**test:[[variable`,
			wantErr: parser.ErrUnmatchedExpression,
		},
		{
			name:  "unmatched expression - missing opening bracket",
			input: `**test:variable]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test", Args: []string{"variable]]"}},
				},
			},
		},
		{
			name:  "expression with nested brackets (should not parse as expression)",
			input: `**test:[[outer[[inner]]outer]]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test", Args: []string{"[[outer[[inner]]outer]]"}},
				},
			},
		},
		{
			name:  "single brackets (not expressions)",
			input: `**test:[single],bracket]`,
			want: parser.Script{
				Cmds: []parser.Command{
					{Name: "test", Args: []string{"[single]", "bracket]"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parser.NewParser(tt.input)
			got, err := p.ParseScript()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ParseScript() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseScript() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPostProcess(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "empty arg",
			input: "",
			want:  "",
		},
		{
			name:  "value only",
			input: "test",
			want:  "test",
		},
		{
			name:  "expression only",
			input: "[[ 2 + 2 + 2 ]]",
			want:  "6",
		},
		{
			name:  "test expression 1",
			input: `something [[platform]]`,
			want:  `something mister`,
		},
		{
			name:  "test expression 2",
			input: `something [[2+2]]`,
			want:  `something 4`,
		},
		{
			name:  "test expression 2 with spacing",
			input: `something [[ 2 + 2 ]]`,
			want:  `something 4`,
		},
		{
			name:  "test expression bool 1",
			input: `something [[true]]`,
			want:  `something true`,
		},
		{
			name:  "test expression bool 2",
			input: `something [[ true == false ]]`,
			want:  `something false`,
		},
		{
			name:    "bad return type",
			input:   `[[device]]`,
			wantErr: parser.ErrBadExpressionReturn,
		},
		{
			name:  "test expression int",
			input: `[[5+5]]`,
			want:  `10`,
		},
		{
			name:  "test expression float 1",
			input: `[[2.5]]`,
			want:  `2.5`,
		},
		{
			name:  "test expression float 2 precision",
			input: `[[1/5]]`,
			want:  `0.2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := parser.ExprEnv{
				Platform:     "mister",
				Version:      "1.2.3",
				MediaPlaying: true,
				ScanMode:     "tap",
				Device: parser.ExprEnvDevice{
					Hostname: "test-device",
					OS:       "linux",
					Arch:     "arm",
				},
			}

			p := parser.NewParser(tt.input)
			got, err := p.PostProcess(env)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("PostProcess() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PostProcess() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
